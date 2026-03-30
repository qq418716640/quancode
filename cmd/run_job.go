package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/config"
	qcontext "github.com/qq418716640/quancode/context"
	"github.com/qq418716640/quancode/job"
	"github.com/qq418716640/quancode/ledger"
	"github.com/qq418716640/quancode/router"
	"github.com/qq418716640/quancode/version"
	"github.com/spf13/cobra"
)

var runJobID string

var runJobCmd = &cobra.Command{
	Use:    "_run-job",
	Hidden: true,
	Short:  "Internal: execute an async job (invoked by delegate --async)",
	RunE:   runJobMain,
}

func runJobMain(cmd *cobra.Command, args []string) error {
	if runJobID == "" {
		return fmt.Errorf("--job-id is required")
	}

	state, err := job.ReadState(runJobID)
	if err != nil {
		return fmt.Errorf("read job state: %w", err)
	}
	if state.Status != job.StatusPending {
		return fmt.Errorf("job %s is in status %q, expected pending", runJobID, state.Status)
	}

	state.Status = job.StatusRunning
	state.PID = os.Getpid()
	pidStartTime, _ := job.GetProcessStartTime(os.Getpid())
	state.PIDStartTime = pidStartTime
	state.RunnerVersion = version.Version
	if err := job.WriteState(state); err != nil {
		// Can't mark failed either since WriteState itself failed.
		return fmt.Errorf("update job to running: %w", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return markFailed(state, job.ErrCodeSpawnFailed, fmt.Sprintf("load config: %v", err))
	}

	agentKey := state.Agent
	ac, ok := cfg.Agents[agentKey]
	if !ok {
		return markFailed(state, job.ErrCodeRouteFailed, fmt.Sprintf("unknown agent: %s", agentKey))
	}
	if !ac.Enabled {
		return markFailed(state, job.ErrCodeRouteFailed, fmt.Sprintf("agent %s is disabled", agentKey))
	}
	// Override agent timeout with the job's effective timeout.
	ac.TimeoutSecs = state.EffectiveTimeout
	// Append non-interactive args for async mode.
	if len(ac.NonInteractiveArgs) > 0 {
		ac.DelegateArgs = append(ac.DelegateArgs, ac.NonInteractiveArgs...)
	}
	a := agent.FromConfig(agentKey, ac)
	if avail, _ := a.IsAvailable(); !avail {
		return markFailed(state, job.ErrCodeSpawnFailed, fmt.Sprintf("agent %s: command %q not found", agentKey, ac.Command))
	}

	var ctxPrefix string
	builder := qcontext.NewBuilder(cfg.ContextDefaults, ac.Context)
	bundle := builder.Build(state.WorkDir, nil, "", 0)
	ctxPrefix = qcontext.Format(bundle)

	// Handle SIGTERM: mark as cancelled and exit.
	// The agent subprocess runs with its own timeout (effective_timeout maps
	// to the agent's timeout_secs), so we don't need an outer context.WithTimeout.
	// Instead, we handle SIGTERM for graceful cancellation.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	cancelled := make(chan struct{})
	var cancelOnce sync.Once
	closeCancelled := func() { cancelOnce.Do(func() { close(cancelled) }) }
	go func() {
		select {
		case <-sigCh:
			closeCancelled()
		case <-cancelled:
		}
	}()

	// Run the delegation attempt (quiet mode — no UI output).
	// The agent's own timeout_secs controls execution time.
	doneCh := make(chan attemptResult, 1)
	go func() {
		ar := runDelegateAttempt(DelegateAttemptOptions{
			Agent:     a,
			AgentKey:  agentKey,
			Task:      state.Task,
			CtxPrefix: ctxPrefix,
			WorkDir:   state.WorkDir,
			Isolation: state.Isolation,
			Quiet:     true,
		})
		doneCh <- ar
	}()

	// Wait for completion or cancellation signal.
	var ar attemptResult
	select {
	case ar = <-doneCh:
		closeCancelled()
	case <-cancelled:
		select {
		case ar = <-doneCh:
			// Agent finished during grace period.
		case <-time.After(15 * time.Second):
			return markCancelled(state)
		}
	}

	// Handle fallback: try other agents if eligible.
	if ar.failureClass != "" && isTransientFailure(ar.failureClass) {
		tried := map[string]bool{agentKey: true}
		for attempt := 2; attempt <= 3; attempt++ {
			// Check if cancelled.
			select {
			case <-cancelled:
				return markCancelled(state)
			default:
			}

			sel := router.SelectAgentExcluding(cfg, state.Task, tried)
			if sel == nil {
				break
			}
			tried[sel.AgentKey] = true
			nextAc := cfg.Agents[sel.AgentKey]
			nextA := agent.FromConfig(sel.AgentKey, nextAc)
			if avail, _ := nextA.IsAvailable(); !avail {
				continue
			}

			nextBuilder := qcontext.NewBuilder(cfg.ContextDefaults, nextAc.Context)
			nextBundle := nextBuilder.Build(state.WorkDir, nil, "", 0)
			nextCtxPrefix := qcontext.Format(nextBundle)

			ar = runDelegateAttempt(DelegateAttemptOptions{
				Agent:     nextA,
				AgentKey:  sel.AgentKey,
				Task:      state.Task,
				CtxPrefix: nextCtxPrefix,
				WorkDir:   state.WorkDir,
				Isolation: state.Isolation,
				Quiet:     true,
			})
			agentKey = sel.AgentKey
			if !isTransientFailure(ar.failureClass) {
				break
			}
		}
	}

	return writeJobResult(state, agentKey, ar)
}

// markFailed writes a terminal failed status to the job state file.
func markFailed(state *job.State, errCode, errMsg string) error {
	state.Status = job.StatusFailed
	state.ErrorCode = errCode
	state.Error = errMsg
	state.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if err := job.WriteState(state); err != nil {
		return fmt.Errorf("write failed state: %w (original error: %s)", err, errMsg)
	}
	return nil
}

// markCancelled writes a cancelled status to the job state file.
func markCancelled(state *job.State) error {
	state.Status = job.StatusCancelled
	state.ErrorCode = job.ErrCodeCancelled
	state.Error = "cancelled by signal"
	state.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	_ = job.WriteState(state)
	return nil
}

// writeJobResult writes the final delegation result into the job state.
func writeJobResult(state *job.State, actualAgent string, ar attemptResult) error {
	now := time.Now().UTC().Format(time.RFC3339)
	state.FinishedAt = now
	state.ActualAgent = actualAgent

	// Determine status.
	if ar.err != nil && ar.result == nil {
		state.Status = job.StatusFailed
		state.ErrorCode = job.ErrCodeSpawnFailed
		state.Error = ar.err.Error()
	} else if ar.result != nil && ar.result.TimedOut {
		state.Status = job.StatusTimedOut
		state.ErrorCode = job.ErrCodeTimeout
	} else if ar.result != nil && ar.result.ExitCode != 0 {
		state.Status = job.StatusFailed
		state.ErrorCode = job.ErrCodeAgentError
		// Include stderr as error context for non-zero exits.
		if ar.err != nil {
			state.Error = ar.err.Error()
		} else if ar.stderr != "" {
			state.Error = truncateForField(ar.stderr, 2048)
		}
	} else if ar.verify.IsStrictFailure() {
		state.Status = job.StatusFailed
		state.ErrorCode = job.ErrCodeVerifyFailed
	} else if ar.patchApplyErr != nil {
		state.Status = job.StatusFailed
		state.ErrorCode = job.ErrCodeWorktree
		state.Error = ar.patchApplyErr.Error()
	} else {
		state.Status = job.StatusSucceeded
	}

	// Collect result data.
	if ar.result != nil {
		exitCode := ar.result.ExitCode
		state.ExitCode = &exitCode
		state.DelegationID = ar.result.DelegationID
	}
	state.ChangedFiles = ar.changedFiles

	// Write agent output to file (capped).
	if ar.output != "" {
		outputWriter, err := job.NewCappedFile(state.OutputFile, job.DefaultMaxOutputBytes)
		if err == nil {
			defer outputWriter.Close()
			outputWriter.Write([]byte(ar.output))
			if ar.stderr != "" && ar.stderr != ar.output {
				outputWriter.Write([]byte("\n--- stderr ---\n"))
				outputWriter.Write([]byte(ar.stderr))
			}
		}
	}

	// Store patch file if produced.
	if ar.patch != "" {
		patchPath := job.PatchPath(state.JobID)
		if err := os.WriteFile(patchPath, []byte(ar.patch), 0644); err == nil {
			state.PatchFile = patchPath
		}
	}

	// Serialize verify result once for both state and ledger.
	var verifyJSON json.RawMessage
	if ar.verify != nil && ar.verify.Enabled {
		if data, err := json.Marshal(ar.verify); err == nil {
			verifyJSON = data
			state.VerifyRaw = verifyJSON
		}
	}

	writeLedger(state, ar, verifyJSON)

	// Write final state (including LedgerWrittenAt if ledger succeeded).
	if err := job.WriteState(state); err != nil {
		return fmt.Errorf("write final job state: %w", err)
	}

	return nil
}

// writeLedger records the async job result in the ledger.
// Sets state.LedgerWrittenAt on success (before the terminal state write).
func writeLedger(state *job.State, ar attemptResult, verifyJSON json.RawMessage) {
	entry := &ledger.Entry{
		Agent:     state.ActualAgent,
		Task:      state.Task,
		WorkDir:   state.WorkDir,
		Isolation: state.Isolation,
	}
	if ar.result != nil {
		entry.ExitCode = ar.result.ExitCode
		entry.TimedOut = ar.result.TimedOut
		entry.DurationMs = ar.result.DurationMs
		entry.ChangedFiles = ar.changedFiles
	}
	entry.FinalStatus = state.Status
	entry.FailureClass = ar.failureClass
	entry.VerifyRaw = verifyJSON

	if err := ledger.Append(entry); err == nil {
		state.LedgerWrittenAt = time.Now().UTC().Format(time.RFC3339)
	}
}

// truncateForField truncates a string to maxLen bytes for storage in state fields.
func truncateForField(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...[truncated]"
}

func init() {
	runJobCmd.Flags().StringVar(&runJobID, "job-id", "", "job ID to execute")
	rootCmd.AddCommand(runJobCmd)
}
