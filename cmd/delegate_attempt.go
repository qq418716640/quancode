package cmd

import (
	"context"
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



// DelegateAttemptOptions controls the behavior of runDelegateAttempt.
type DelegateAttemptOptions struct {
	Agent           agent.Agent
	AgentKey        string
	Task            string
	CtxPrefix       string
	WorkDir         string
	Isolation       string
	Verify          *verifySpec
	TimeoutOverride int // per-task timeout override in seconds; 0 means use agent default
	// Quiet suppresses UI output (spinner, stderr messages).
	// Used by async job-runner where there is no terminal.
	Quiet bool
	// Speculative parallelism fields
	Ctx             context.Context // external context for cancellation; nil = use agent's internal timeout
	DeferPatchApply bool            // collect patch but don't apply to main tree
	DeferVerify     bool            // skip verification (caller will verify the winner)
}

// runDelegateAttempt executes one delegation attempt against a single agent,
// including worktree setup, verification, and result collection.
func runDelegateAttempt(opts DelegateAttemptOptions) (ar attemptResult) {
	defer func() {
		ar.failureClass = classifyFailure(ar)
	}()

	logf := func(format string, args ...any) {
		if !opts.Quiet {
			fmt.Fprintf(os.Stderr, format, args...)
		}
	}

	// Clean up orphan worktrees from previous crashed runs
	if pruned := runner.PruneOrphanWorktrees(opts.WorkDir); pruned > 0 {
		logf("[quancode] cleaned %d orphan worktree(s)\n", pruned)
	}

	execDir := opts.WorkDir
	var cleanupWorktree func()

	if opts.Isolation == "worktree" || opts.Isolation == "patch" {
		if !runner.IsGitRepo(opts.WorkDir) {
			ar.err = fmt.Errorf("--isolation %s requires a git repository", opts.Isolation)
			return ar
		}
		wt, cleanup, wtErr := runner.CreateWorktree(opts.WorkDir)
		if wtErr != nil {
			ar.err = fmt.Errorf("create worktree: %w", wtErr)
			return ar
		}
		cleanupWorktree = cleanup
		defer func() {
			if cleanupWorktree != nil {
				cleanupWorktree()
			}
		}()
		execDir = wt
		logf("[quancode] running in isolated worktree: %s\n", wt)
	}

	ar.preSnapshot = gitStatusSnapshot(execDir)

	if !opts.Quiet {
		ui.DelegationStart(opts.AgentKey, opts.Task, opts.Isolation)
	}
	delegationID, err := ledger.NewDelegationID()
	if err != nil {
		ar.err = fmt.Errorf("generate delegation id: %w", err)
		return ar
	}

	// Assemble full task with context prefix
	fullTask := opts.Task
	if opts.CtxPrefix != "" {
		fullTask = opts.CtxPrefix + "\n\n=== TASK ===\n\n" + opts.Task
	}

	var spinner *ui.Spinner
	if !opts.Quiet {
		spinner = ui.NewSpinner(fmt.Sprintf("%s working...", opts.AgentKey))
		defer spinner.Stop()
	}

	delegateOpts := agent.DelegateOptions{
		DelegationID:    delegationID,
		TimeoutOverride: opts.TimeoutOverride,
	}
	var result *runner.Result
	var delegateErr error
	if opts.Ctx != nil {
		result, delegateErr = opts.Agent.DelegateWithContext(opts.Ctx, execDir, fullTask, delegateOpts)
	} else {
		result, delegateErr = opts.Agent.Delegate(execDir, fullTask, delegateOpts)
	}
	if spinner != nil {
		spinner.Stop()
	}
	ar.result = result
	ar.err = delegateErr

	// Collect patch from worktree
	if opts.Isolation == "worktree" || opts.Isolation == "patch" {
		var collectErr error
		ar.patch, ar.patchFiles, collectErr = runner.CollectPatch(execDir)
		if collectErr != nil {
			logf("[quancode] warning: patch collection failed: %v\n", collectErr)
		}
		// Cache patch for later apply-patch --id
		if opts.Isolation == "patch" && ar.patch != "" {
			if _, cacheErr := runner.CachePatch(delegationID, ar.patch); cacheErr != nil {
				debugf("patch cache failed: %v", cacheErr)
			}
		}
	}

	// Run verification only when agent succeeded — skip on timeout/failure
	// to avoid blocking fallback due to unrelated baseline test failures.
	// DeferVerify skips verification (speculative mode — caller verifies the winner).
	if !opts.DeferVerify && result != nil && !result.TimedOut && result.ExitCode == 0 {
		if opts.Quiet {
			if opts.Verify != nil && len(opts.Verify.Commands) > 0 {
				ar.verify = runVerification(execDir, opts.Verify.Commands, opts.Verify.TimeoutSec, opts.Verify.Strict)
			}
		} else {
			ar.verify = runAndLogVerification(execDir, opts.Verify)
		}
	}

	// Apply patch to main tree (worktree mode only).
	// DeferPatchApply skips application (speculative mode — caller applies the winner's patch).
	if !opts.DeferPatchApply && opts.Isolation == "worktree" && ar.patch != "" {
		if ar.verify.IsStrictFailure() {
			logf("[quancode] patch NOT applied (verify-strict failed)\n")
		} else {
			conflicts := runner.CheckPatchConflicts(opts.WorkDir, ar.patch)
			if len(conflicts) > 0 {
				ar.patchApplyErr = fmt.Errorf("patch conflicts with %d files", len(conflicts))
				ar.conflictFiles = conflicts
			} else if applyErr := runner.ApplyPatch(opts.WorkDir, ar.patch); applyErr != nil {
				ar.patchApplyErr = applyErr
			} else {
				logf("[quancode] changes applied to working directory\n")
			}
		}
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
		} else if opts.Isolation == "" || opts.Isolation == "inplace" {
			ar.changedFiles = detectNewChanges(opts.WorkDir, ar.preSnapshot)
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

