package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/ledger"
	"github.com/qq418716640/quancode/runner"
	"github.com/qq418716640/quancode/ui"
)

// verifySpec holds the parsed verification configuration for a delegation.
type verifySpec struct {
	Commands   []string `json:"commands"`
	Strict     bool     `json:"strict"`
	TimeoutSec int      `json:"timeout_sec"`
}

// attemptResult holds everything produced by a single delegation attempt.
type attemptResult struct {
	result       *runner.Result
	err          error
	output       string
	stderr       string
	patch        string
	patchFiles   []string
	changedFiles []string
	preSnapshot  map[string]bool
	verify       *VerifyResult
	// Patch apply failure details (worktree mode only)
	patchApplyErr error
	conflictFiles []string
	failureClass  string
}



// runDelegateAttempt executes one delegation attempt against a single agent,
// including worktree setup, verification, and result collection.
// ctxPrefix is the formatted context to prepend to the task (may be empty).
func runDelegateAttempt(a agent.Agent, agentKey, task, ctxPrefix, workDir, isolation string, vs *verifySpec) (ar attemptResult) {
	defer func() {
		ar.failureClass = classifyFailure(ar)
	}()

	// Clean up orphan worktrees from previous crashed runs
	if pruned := runner.PruneOrphanWorktrees(workDir); pruned > 0 {
		fmt.Fprintf(os.Stderr, "[quancode] cleaned %d orphan worktree(s)\n", pruned)
	}

	execDir := workDir
	var cleanupWorktree func()

	if isolation == "worktree" || isolation == "patch" {
		if !runner.IsGitRepo(workDir) {
			ar.err = fmt.Errorf("--isolation %s requires a git repository", isolation)
			return ar
		}
		wt, cleanup, wtErr := runner.CreateWorktree(workDir)
		if wtErr != nil {
			ar.err = fmt.Errorf("create worktree: %w", wtErr)
			return ar
		}
		cleanupWorktree = cleanup
		// Ensure worktree is cleaned up even if later setup steps fail.
		defer func() {
			if cleanupWorktree != nil {
				cleanupWorktree()
			}
		}()
		execDir = wt
		fmt.Fprintf(os.Stderr, "[quancode] running in isolated worktree: %s\n", wt)
	}

	ar.preSnapshot = gitStatusSnapshot(execDir)

	ui.DelegationStart(agentKey, task, isolation)
	delegationID, err := ledger.NewDelegationID()
	if err != nil {
		ar.err = fmt.Errorf("generate delegation id: %w", err)
		return ar
	}

	// Assemble full task with context prefix
	fullTask := task
	if ctxPrefix != "" {
		fullTask = ctxPrefix + "\n\n=== TASK ===\n\n" + task
	}

	spinner := ui.NewSpinner(fmt.Sprintf("%s working...", agentKey))
	defer spinner.Stop() // safety net for panic
	result, delegateErr := a.Delegate(execDir, fullTask, agent.DelegateOptions{
		DelegationID: delegationID,
	})
	spinner.Stop()
	ar.result = result
	ar.err = delegateErr

	// Collect patch from worktree
	if isolation == "worktree" || isolation == "patch" {
		var collectErr error
		ar.patch, ar.patchFiles, collectErr = runner.CollectPatch(execDir)
		if collectErr != nil {
			fmt.Fprintf(os.Stderr, "[quancode] warning: patch collection failed: %v\n", collectErr)
		}
		// Cache patch for later apply-patch --id
		if isolation == "patch" && ar.patch != "" {
			if _, cacheErr := runner.CachePatch(delegationID, ar.patch); cacheErr != nil {
				debugf("patch cache failed: %v", cacheErr)
			}
		}
	}

	// Run verification only when agent succeeded — skip on timeout/failure
	// to avoid blocking fallback due to unrelated baseline test failures.
	if result != nil && !result.TimedOut && result.ExitCode == 0 {
		ar.verify = runAndLogVerification(execDir, vs)
	}

	// Apply patch to main tree (worktree mode only)
	if isolation == "worktree" && ar.patch != "" {
		if ar.verify.IsStrictFailure() {
			fmt.Fprintf(os.Stderr, "[quancode] patch NOT applied (verify-strict failed)\n")
		} else {
			// Pre-check for conflicts before applying to avoid polluting the work tree
			conflicts := runner.CheckPatchConflicts(workDir, ar.patch)
			if len(conflicts) > 0 {
				ar.patchApplyErr = fmt.Errorf("patch conflicts with %d files", len(conflicts))
				ar.conflictFiles = conflicts
			} else if applyErr := runner.ApplyPatch(workDir, ar.patch); applyErr != nil {
				ar.patchApplyErr = applyErr
			} else {
				fmt.Fprintf(os.Stderr, "[quancode] changes applied to working directory\n")
			}
		}
	}

	if isolation == "worktree" || isolation == "patch" {
		cleanupWorktree = nil // prevent double cleanup
	}

	// Build output
	if result != nil {
		ar.output = result.Stdout
		ar.stderr = result.Stderr
		if ar.output == "" {
			ar.output = result.Stderr
		}
		if len(ar.patchFiles) > 0 {
			ar.changedFiles = ar.patchFiles
		} else if isolation == "" || isolation == "inplace" {
			ar.changedFiles = detectNewChanges(workDir, ar.preSnapshot)
		}
	}

	return ar
}

// attemptMeta carries run-level tracking state across the fallback loop.
type attemptMeta struct {
	RunID          string
	Attempt        int
	FallbackFrom   string
	FallbackReason string
}

// logAttempt writes a ledger entry for a single attempt.
func logAttempt(agentKey, task, workDir, isolation string, meta attemptMeta, ar attemptResult) {
	logEntry := &ledger.Entry{
		Agent:          agentKey,
		Task:           task,
		WorkDir:        workDir,
		Isolation:      isolation,
		RunID:          meta.RunID,
		Attempt:        meta.Attempt,
		FallbackFrom:   meta.FallbackFrom,
		FallbackReason: meta.FallbackReason,
	}
	if ar.result != nil {
		logEntry.ExitCode = ar.result.ExitCode
		logEntry.TimedOut = ar.result.TimedOut
		logEntry.DurationMs = ar.result.DurationMs
		logEntry.ChangedFiles = ar.changedFiles
	}
	logEntry.FailureClass = ar.failureClass
	logEntry.ConflictFiles = ar.conflictFiles
	if ar.patchApplyErr != nil {
		logEntry.ChangedFiles = nil // patch was not applied to the main tree
	}
	if (ar.err != nil || ar.patchApplyErr != nil) && logEntry.ExitCode == 0 {
		logEntry.ExitCode = 1
	}

	// Write verification result and final status
	if ar.verify != nil && ar.verify.Enabled {
		if data, err := json.Marshal(ar.verify); err == nil {
			logEntry.VerifyRaw = data
		}
	}
	logEntry.FinalStatus = determineFinalStatus(logEntry.ExitCode, logEntry.TimedOut, ar.verify)

	if logErr := ledger.Append(logEntry); logErr != nil {
		fmt.Fprintf(os.Stderr, "[quancode] warning: failed to write ledger: %v\n", logErr)
	}
}

// finalizeDelegation handles output formatting and ledger recording for the final attempt.
func finalizeDelegation(agentKey, task, workDir, isolation string, meta attemptMeta, ar attemptResult) error {
	// Record to ledger
	logAttempt(agentKey, task, workDir, isolation, meta, ar)

	// Ceremony: show completion/failure summary
	if ar.failureClass == "" && ar.err == nil && ar.patchApplyErr == nil {
		var durationMs int64
		if ar.result != nil {
			durationMs = ar.result.DurationMs
		}
		ui.DelegationSuccess(agentKey, durationMs, len(ar.changedFiles))
	} else if ar.result != nil {
		ui.DelegationFailure(agentKey, ar.result.DurationMs, ar.failureClass)
	}

	verifyStrictFailed := ar.verify.IsStrictFailure()

	hasPatchApplyErr := ar.patchApplyErr != nil

	if delegateFormat == "json" {
		dr := buildDelegationResult(agentKey, task, isolation, ar)
		data, _ := json.MarshalIndent(dr, "", "  ")
		fmt.Println(string(data))
		if ar.err != nil || verifyStrictFailed || hasPatchApplyErr {
			return &agent.ExitStatusError{Code: 1}
		}
		return nil
	}

	// Text format
	if ar.err != nil {
		fmt.Fprintf(os.Stderr, "[quancode] delegation error: %v\n", ar.err)
		if ar.output != "" {
			fmt.Print(ar.output)
		}
		return &agent.ExitStatusError{Code: 1}
	}
	if hasPatchApplyErr {
		fmt.Fprintf(os.Stderr, "[quancode] patch apply failed: %v\n", ar.patchApplyErr)
		if len(ar.conflictFiles) > 0 {
			fmt.Fprintf(os.Stderr, "[quancode] conflicts:\n")
			for _, f := range ar.conflictFiles {
				fmt.Fprintf(os.Stderr, "[quancode]   - %s\n", f)
			}
		}
		fmt.Fprintf(os.Stderr, "[quancode] to apply manually: save the patch below and run: git apply --3way <file>\n")
		if ar.output != "" {
			fmt.Print(ar.output)
		}
		fmt.Print(ar.patch)
		return &agent.ExitStatusError{Code: 1}
	}
	if verifyStrictFailed {
		fmt.Fprintf(os.Stderr, "[quancode] delegation failed: verify-strict failed (%d/%d commands)\n", ar.verify.FailedCount, len(ar.verify.Commands))
		if ar.output != "" {
			fmt.Print(ar.output)
		}
		return &agent.ExitStatusError{Code: 1}
	}
	if isolation == "patch" && ar.patch != "" {
		fmt.Fprintf(os.Stderr, "[quancode] patch (%d files changed, not applied):\n", len(ar.patchFiles))
		fmt.Print(ar.patch)
		return nil
	}
	fmt.Print(ar.output)
	return nil
}

