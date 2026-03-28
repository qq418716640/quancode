package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/approval"
	"github.com/qq418716640/quancode/ledger"
	"github.com/qq418716640/quancode/runner"
)

// verifySpec holds the parsed verification configuration for a delegation.
type verifySpec struct {
	Commands   []string `json:"commands"`
	Strict     bool     `json:"strict"`
	TimeoutSec int      `json:"timeout_sec"`
}

// attemptResult holds everything produced by a single delegation attempt.
type attemptResult struct {
	result         *runner.Result
	err            error
	output         string
	stderr         string
	patch          string
	patchFiles     []string
	changedFiles   []string
	approvalEvents []ledger.ApprovalEvent
	preSnapshot    map[string]bool
	verify         *VerifyResult
	// Patch apply failure details (worktree mode only)
	patchApplyErr   error
	conflictFiles   []string
}

// runDelegateAttempt executes one delegation attempt against a single agent,
// including worktree setup, approval polling, verification, and result collection.
// ctxPrefix is the formatted context to prepend to the task (may be empty).
func runDelegateAttempt(a agent.Agent, agentKey, task, ctxPrefix, workDir, isolation string, vs *verifySpec) attemptResult {
	var ar attemptResult

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

	fmt.Fprintf(os.Stderr, "[quancode] delegating to %s: %s\n", agentKey, task)
	delegationID, err := approval.NewDelegationID()
	if err != nil {
		ar.err = fmt.Errorf("generate delegation id: %w", err)
		return ar
	}
	approvalDir, err := approval.CreateApprovalDir(delegationID)
	if err != nil {
		ar.err = fmt.Errorf("create approval dir: %w", err)
		return ar
	}
	defer func() {
		if cleanupErr := approval.CleanupApprovalDir(approvalDir); cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "[quancode] warning: failed to clean approval dir: %v\n", cleanupErr)
		}
	}()

	// Assemble full task with context prefix
	fullTask := task
	if ctxPrefix != "" {
		fullTask = ctxPrefix + "\n\n=== TASK ===\n\n" + task
	}

	// Run agent in background
	type delegateResult struct {
		result *runner.Result
		err    error
	}
	doneCh := make(chan delegateResult, 1)
	go func() {
		result, err := a.Delegate(execDir, fullTask, agent.DelegateOptions{
			DelegationID: delegationID,
			ApprovalDir:  approvalDir,
		})
		doneCh <- delegateResult{result: result, err: err}
	}()

	// Approval polling loop
	var result *runner.Result
	pendingSince := make(map[string]time.Time)
	eventIndex := make(map[string]int)
	pollTicker := time.NewTicker(approvalPollInterval)
	defer pollTicker.Stop()

	promptQueue := make(chan *approval.Request, 16)
	loopDone := make(chan struct{})

	reader := stdinReader
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}

	type stdinResult struct {
		line string
		err  error
	}
	readLine := func() <-chan stdinResult {
		ch := make(chan stdinResult, 1)
		go func() {
			line, err := reader.ReadString('\n')
			ch <- stdinResult{line, err}
		}()
		return ch
	}

	go func() {
		for {
			select {
			case <-loopDone:
				return
			case pr := <-promptQueue:
				fmt.Fprintf(os.Stderr, "\n[quancode] approval requested: %s\n", pr.RequestID)
				fmt.Fprintf(os.Stderr, "  action:      %s\n", pr.Action)
				fmt.Fprintf(os.Stderr, "  description: %s\n", pr.Description)
				fmt.Fprintf(os.Stderr, "  approve? [y/n]: ")

				var answer string
				select {
				case <-loopDone:
					return
				case res := <-readLine():
					if res.err != nil {
						continue
					}
					answer = strings.TrimSpace(strings.ToLower(res.line))
				}

				var decision, reason string
				switch answer {
				case "y", "yes":
					decision, reason = "approved", "user approved interactively"
				case "n", "no":
					decision, reason = "denied", "user denied interactively"
				default:
					decision = "denied"
					reason = fmt.Sprintf("unrecognised input %q, treated as deny", answer)
					fmt.Fprintf(os.Stderr, "[quancode] unrecognised input %q — treating as deny\n", answer)
				}
				writeErr := approval.WriteResponse(approvalDir, approval.Response{
					RequestID: pr.RequestID,
					Decision:  decision,
					DecidedBy: "user",
					Reason:    reason,
				})
				if writeErr != nil && !errors.Is(writeErr, approval.ErrResponseExists) {
					fmt.Fprintf(os.Stderr, "[quancode] warning: failed to write approval response: %v\n", writeErr)
				}
			}
		}
	}()

loop:
	for {
		select {
		case done := <-doneCh:
			result = done.result
			ar.err = done.err
			break loop
		case <-pollTicker.C:
			pending, pollErr := approval.PendingRequests(approvalDir)
			if pollErr != nil {
				fmt.Fprintf(os.Stderr, "[quancode] warning: approval poll failed: %v\n", pollErr)
				continue
			}
			now := time.Now()
			for _, req := range pending {
				if _, ok := pendingSince[req.RequestID]; !ok {
					pendingSince[req.RequestID] = now
					eventIndex[req.RequestID] = len(ar.approvalEvents)
					ar.approvalEvents = append(ar.approvalEvents, ledger.ApprovalEvent{
						RequestID:   req.RequestID,
						Action:      req.Action,
						Description: req.Description,
					})

					if delegateAutoApprove {
						fmt.Fprintf(os.Stderr, "[quancode] auto-approved: %s %s\n", req.RequestID, req.Description)
						writeErr := approval.WriteResponse(approvalDir, approval.Response{
							RequestID: req.RequestID,
							Decision:  "approved",
							DecidedBy: "auto",
							Reason:    "auto-approved via --auto-approve",
						})
						if writeErr != nil && !errors.Is(writeErr, approval.ErrResponseExists) {
							fmt.Fprintf(os.Stderr, "[quancode] warning: failed to write auto-approval: %v\n", writeErr)
						}
					} else {
						promptQueue <- req
					}
				}
				if now.Sub(pendingSince[req.RequestID]) >= approvalTimeout {
					_, existsErr := approval.ReadResponse(approvalDir, req.RequestID)
					if existsErr == nil {
						continue
					}
					if !os.IsNotExist(existsErr) {
						fmt.Fprintf(os.Stderr, "[quancode] warning: approval response check failed: %v\n", existsErr)
						continue
					}
					writeErr := approval.WriteResponse(approvalDir, approval.Response{
						RequestID: req.RequestID,
						Decision:  "denied",
						DecidedBy: "timeout",
						Reason:    "approval timed out",
					})
					if writeErr != nil {
						if errors.Is(writeErr, approval.ErrResponseExists) {
							fmt.Fprintf(os.Stderr, "[quancode] approval %s was decided before timeout denial could be written\n", req.RequestID)
						} else {
							fmt.Fprintf(os.Stderr, "[quancode] warning: failed to write timeout denial: %v\n", writeErr)
						}
					}
				}
			}
		}
	}
	close(loopDone)

	// Read final approval decisions
	for requestID, idx := range eventIndex {
		resp, readErr := approval.ReadResponse(approvalDir, requestID)
		if readErr == nil {
			ar.approvalEvents[idx].Decision = resp.Decision
		}
	}

	ar.result = result

	// Collect patch from worktree
	if isolation == "worktree" || isolation == "patch" {
		var collectErr error
		ar.patch, ar.patchFiles, collectErr = runner.CollectPatch(execDir)
		if collectErr != nil {
			fmt.Fprintf(os.Stderr, "[quancode] warning: patch collection failed: %v\n", collectErr)
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
	logEntry.ApprovalEvents = append(logEntry.ApprovalEvents, ar.approvalEvents...)
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

