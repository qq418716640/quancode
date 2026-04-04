package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/config"
	qcontext "github.com/qq418716640/quancode/context"
	"github.com/qq418716640/quancode/ledger"
	"github.com/qq418716640/quancode/router"
	"github.com/qq418716640/quancode/runner"
	"github.com/qq418716640/quancode/ui"
)

// errNoSpeculativeAgent is returned when no backup agent is available for speculative execution.
var errNoSpeculativeAgent = errors.New("no speculative agent available")

// speculativeResult wraps an attemptResult with agent identification.
type speculativeResult struct {
	agentKey string
	ar       attemptResult
	role     string // "primary" or "speculative"
}

// speculativeDelegationOpts holds the options for speculative delegation.
type speculativeDelegationOpts struct {
	cfg             *config.Config
	primaryAgent    agent.Agent
	primaryKey      string
	task            string
	workDir         string
	isolation       string
	verify          *verifySpec
	timeoutOverride int
	delaySecs       int
	noContext       bool
	contextFiles    []string
	contextDiff     string
	contextMaxSize  int
}

// runSpeculativeDelegation runs the primary agent with a speculative fallback.
// After delaySecs, if the primary is still running, a backup agent is launched
// in parallel. The first successful result wins.
func runSpeculativeDelegation(opts speculativeDelegationOpts) error {
	// Find speculative agent — must be available and support the same
	// isolation mode. Mixed isolation would break race semantics (e.g.
	// worktree applies changes but patch doesn't).
	tried := map[string]bool{opts.primaryKey: true}
	var specSel *router.Selection
	var specAc config.AgentConfig
	var specAgent agent.Agent
	for {
		sel := router.SelectAgentExcluding(opts.cfg, opts.task, tried)
		if sel == nil {
			return errNoSpeculativeAgent
		}
		tried[sel.AgentKey] = true
		ac := opts.cfg.Agents[sel.AgentKey]
		if !ac.SupportsIsolation(opts.isolation) {
			fmt.Fprintf(os.Stderr, "[quancode] skipping %s for speculative (does not support isolation %s)\n",
				sel.AgentKey, opts.isolation)
			continue
		}
		a := agent.FromConfig(sel.AgentKey, ac)
		if ok, _ := a.IsAvailable(); !ok {
			continue
		}
		specSel = sel
		specAc = ac
		specAgent = a
		break
	}

	runID, err := ledger.NewRunID()
	if err != nil {
		return fmt.Errorf("generate run id: %w", err)
	}

	// Build context prefix for primary
	primaryCtxPrefix := buildContextPrefix(opts.cfg, opts.primaryKey, opts)
	// Build context prefix for speculative (may differ per-agent)
	specCtxPrefix := buildContextPrefix(opts.cfg, specSel.AgentKey, opts)

	// Resolve timeouts
	primaryAc := opts.cfg.Agents[opts.primaryKey]
	primaryTimeout := resolveEffectiveTimeout(opts.timeoutOverride, primaryAc.TimeoutSecs)
	specTimeout := resolveEffectiveTimeout(opts.timeoutOverride, specAc.TimeoutSecs)

	resultCh := make(chan speculativeResult, 2)

	// Start primary agent
	primaryCtx, primaryCancel := context.WithTimeout(context.Background(), time.Duration(primaryTimeout)*time.Second)
	defer primaryCancel()

	fmt.Fprintf(os.Stderr, "[quancode] ⚡ Dispatching to %s (speculative backup: %s in %ds)...\n",
		opts.primaryKey, specSel.AgentKey, opts.delaySecs)

	spinner := ui.NewSpinner(fmt.Sprintf("%s working...", opts.primaryKey))

	go func() {
		ar := runDelegateAttempt(DelegateAttemptOptions{
			Agent:           opts.primaryAgent,
			AgentKey:        opts.primaryKey,
			Task:            opts.task,
			CtxPrefix:       primaryCtxPrefix,
			WorkDir:         opts.workDir,
			Isolation:       opts.isolation,
			Verify:          opts.verify,
			TimeoutOverride: opts.timeoutOverride,
			Quiet:           true, // orchestrator manages UI
			Ctx:             primaryCtx,
			DeferPatchApply: true,
			DeferVerify:     true,
		})
		resultCh <- speculativeResult{agentKey: opts.primaryKey, ar: ar, role: "primary"}
	}()

	// Wait for speculative delay or primary completion
	timer := time.NewTimer(time.Duration(opts.delaySecs) * time.Second)

	select {
	case res := <-resultCh:
		timer.Stop()
		spinner.Stop()
		primaryCancel()
		if res.ar.failureClass == "" && res.ar.err == nil {
			// Primary succeeded within window — no speculative needed
			return finalizeSpeculativeWinner(opts, res, nil, runID)
		}
		if !isTransientFailure(res.ar.failureClass) {
			// Non-transient failure (agent_failed, etc.) — no fallback
			return finalizeSpeculativeWinner(opts, res, nil, runID)
		}
		// Transient failure within window — launch speculative immediately
		fmt.Fprintf(os.Stderr, "[quancode] %s %s within window, launching %s immediately...\n",
			opts.primaryKey, res.ar.failureClass, specSel.AgentKey)
		spinner = ui.NewSpinner(fmt.Sprintf("%s working...", specSel.AgentKey))
		// Store primary result, launch speculative below
		primary := &res
		specCtx, specCancel := context.WithTimeout(context.Background(), time.Duration(specTimeout)*time.Second)
		defer specCancel()
		go func() {
			ar := runDelegateAttempt(DelegateAttemptOptions{
				Agent:           specAgent,
				AgentKey:        specSel.AgentKey,
				Task:            opts.task,
				CtxPrefix:       specCtxPrefix,
				WorkDir:         opts.workDir,
				Isolation:       opts.isolation,
				Verify:          opts.verify,
				TimeoutOverride: opts.timeoutOverride,
				Quiet:           true,
				Ctx:             specCtx,
				DeferPatchApply: true,
				DeferVerify:     true,
			})
			resultCh <- speculativeResult{agentKey: specSel.AgentKey, ar: ar, role: "speculative"}
		}()
		specRes := <-resultCh
		spinner.Stop()
		spec := &specRes
		if specRes.ar.failureClass == "" && specRes.ar.err == nil {
			return finalizeSpeculativeWinner(opts, specRes, primary, runID)
		}
		return finalizeSpeculativeBothFailed(opts, primary, spec, runID)

	case <-timer.C:
		// Window expired, primary still running — launch speculative
		spinner.Stop()
		fmt.Fprintf(os.Stderr, "[quancode] %s still running after %ds, launching %s in parallel...\n",
			opts.primaryKey, opts.delaySecs, specSel.AgentKey)
		spinner = ui.NewSpinner(fmt.Sprintf("%s + %s racing...", opts.primaryKey, specSel.AgentKey))
	}

	// Launch speculative agent
	specCtx, specCancel := context.WithTimeout(context.Background(), time.Duration(specTimeout)*time.Second)
	defer specCancel()

	go func() {
		ar := runDelegateAttempt(DelegateAttemptOptions{
			Agent:           specAgent,
			AgentKey:        specSel.AgentKey,
			Task:            opts.task,
			CtxPrefix:       specCtxPrefix,
			WorkDir:         opts.workDir,
			Isolation:       opts.isolation,
			Verify:          opts.verify,
			TimeoutOverride: opts.timeoutOverride,
			Quiet:           true,
			Ctx:             specCtx,
			DeferPatchApply: true,
			DeferVerify:     true,
		})
		resultCh <- speculativeResult{agentKey: specSel.AgentKey, ar: ar, role: "speculative"}
	}()

	// Wait for results — first success wins
	var primary, spec *speculativeResult
	for i := 0; i < 2; i++ {
		res := <-resultCh
		if res.role == "primary" {
			primary = &res
		} else {
			spec = &res
		}

		if res.ar.failureClass == "" && res.ar.err == nil {
			// Winner found — cancel the other
			spinner.Stop()
			if res.role == "primary" {
				specCancel()
				fmt.Fprintf(os.Stderr, "[quancode] %s won the race, cancelling %s\n", opts.primaryKey, specSel.AgentKey)
			} else {
				primaryCancel()
				fmt.Fprintf(os.Stderr, "[quancode] %s won the race, cancelling %s\n", specSel.AgentKey, opts.primaryKey)
			}
			// Drain the other result (goroutine will finish after cancel)
			if i == 0 {
				loser := <-resultCh
				if loser.role == "primary" {
					primary = &loser
				} else {
					spec = &loser
				}
			}
			return finalizeSpeculativeWinner(opts, res, loserOf(primary, spec, res.role), runID)
		}
		// This agent failed — keep waiting for the other
		if i == 0 {
			spinner.Stop()
			remaining := "speculative"
			if res.role == "speculative" {
				remaining = "primary"
			}
			fmt.Fprintf(os.Stderr, "[quancode] %s %s, waiting for %s agent...\n",
				res.agentKey, res.ar.failureClass, remaining)
			spinner = ui.NewSpinner(fmt.Sprintf("waiting for %s agent...", remaining))
		}
	}

	// Both failed
	spinner.Stop()
	fmt.Fprintf(os.Stderr, "[quancode] both agents failed\n")

	// Log both as failed, return the speculative result (last hope)
	return finalizeSpeculativeBothFailed(opts, primary, spec, runID)
}

// loserOf returns the result that didn't win.
func loserOf(primary, spec *speculativeResult, winnerRole string) *speculativeResult {
	if winnerRole == "primary" {
		return spec
	}
	return primary
}

// finalizeSpeculativeWinner handles the winning result: verify, apply patch, log both entries.
func finalizeSpeculativeWinner(opts speculativeDelegationOpts, winner speculativeResult, loser *speculativeResult, runID string) error {
	if opts.isolation == "worktree" && winner.ar.patch != "" {
		conflicts := runner.CheckPatchConflicts(opts.workDir, winner.ar.patch)
		if len(conflicts) > 0 {
			winner.ar.patchApplyErr = fmt.Errorf("patch conflicts with %d files", len(conflicts))
			winner.ar.conflictFiles = conflicts
		} else if applyErr := runner.ApplyPatch(opts.workDir, winner.ar.patch); applyErr != nil {
			winner.ar.patchApplyErr = applyErr
		} else {
			fmt.Fprintf(os.Stderr, "[quancode] changes from %s applied to working directory\n", winner.agentKey)
		}
	}

	// Run verification on the winner (in working directory after patch apply).
	// Skip for patch mode: the patch is not applied to workDir, so verifying
	// the baseline tree would be meaningless.
	if winner.ar.failureClass == "" && winner.ar.err == nil && opts.verify != nil && opts.isolation != "patch" {
		winner.ar.verify = runAndLogVerification(opts.workDir, opts.verify)
	}

	// Reclassify after verify
	winner.ar.failureClass = classifyFailure(winner.ar)

	// Log winner
	winnerMeta := attemptMeta{
		RunID:   runID,
		Attempt: 1,
	}
	logSpeculativeEntry(winner.agentKey, opts.task, opts.workDir, opts.isolation, winnerMeta, winner.ar, winner.role, "")

	// Log loser (if speculative was launched)
	if loser != nil {
		loserMeta := attemptMeta{
			RunID:   runID,
			Attempt: 2,
		}
		logSpeculativeEntry(loser.agentKey, opts.task, opts.workDir, opts.isolation, loserMeta, loser.ar, loser.role, winner.agentKey)
	}

	// UI ceremony
	if winner.ar.failureClass == "" && winner.ar.err == nil && winner.ar.patchApplyErr == nil {
		var durationMs int64
		if winner.ar.result != nil {
			durationMs = winner.ar.result.DurationMs
		}
		ui.DelegationSuccess(winner.agentKey, durationMs, len(winner.ar.changedFiles))
	}

	// Show speculative chain (primary always listed first)
	if loser != nil {
		primaryKey, specKey := winner.agentKey, loser.agentKey
		if loser.role == "primary" {
			primaryKey, specKey = loser.agentKey, winner.agentKey
		}
		fmt.Fprintf(os.Stderr, "[quancode] Speculative: %s + %s → %s won\n",
			primaryKey, specKey, winner.agentKey)
	}

	// Finalize output
	return finalizeSpeculativeOutput(winner, opts.isolation)
}

// finalizeSpeculativeBothFailed handles the case where both agents failed.
func finalizeSpeculativeBothFailed(opts speculativeDelegationOpts, primary, spec *speculativeResult, runID string) error {
	// Log both
	if primary != nil {
		meta := attemptMeta{RunID: runID, Attempt: 1}
		logSpeculativeEntry(primary.agentKey, opts.task, opts.workDir, opts.isolation, meta, primary.ar, "primary", "")
	}
	if spec != nil {
		meta := attemptMeta{RunID: runID, Attempt: 2}
		logSpeculativeEntry(spec.agentKey, opts.task, opts.workDir, opts.isolation, meta, spec.ar, "speculative", "")
	}

	// Use whichever has more useful output
	final := spec
	if final == nil {
		final = primary
	}

	if final.ar.result != nil {
		ui.DelegationFailure(final.agentKey, final.ar.result.DurationMs, final.ar.failureClass)
	}

	return finalizeSpeculativeOutput(*final, opts.isolation)
}

// logSpeculativeEntry writes a ledger entry with speculative tracking fields.
func logSpeculativeEntry(agentKey, task, workDir, isolation string, meta attemptMeta, ar attemptResult, role, cancelledBy string) {
	outputFile := ledger.WriteOutput(ar.delegationID, ar.output, ledger.DefaultMaxOutputBytes)

	logEntry := &ledger.Entry{
		Agent:           agentKey,
		Task:            task,
		WorkDir:         workDir,
		Isolation:       isolation,
		DelegationID:    ar.delegationID,
		OutputFile:      outputFile,
		RunID:           meta.RunID,
		Attempt:         meta.Attempt,
		FallbackFrom:    meta.FallbackFrom,
		FallbackReason:  meta.FallbackReason,
		Speculative:     true,
		SpeculativeRole: role,
		CancelledBy:     cancelledBy,
	}
	if ar.result != nil {
		logEntry.ExitCode = ar.result.ExitCode
		logEntry.TimedOut = ar.result.TimedOut
		logEntry.DurationMs = ar.result.DurationMs
		logEntry.ChangedFiles = ar.changedFiles
	}
	logEntry.FailureClass = ar.failureClass
	if cancelledBy != "" {
		logEntry.FailureClass = FailureClassSpeculativeCancelled
	}
	logEntry.FinalStatus = determineFinalStatus(logEntry.ExitCode, logEntry.TimedOut, ar.verify)
	if cancelledBy != "" {
		logEntry.FinalStatus = "cancelled"
	}

	if logErr := ledger.Append(logEntry); logErr != nil {
		fmt.Fprintf(os.Stderr, "[quancode] warning: failed to write ledger: %v\n", logErr)
	}
}

// finalizeSpeculativeOutput handles the output formatting for speculative delegation.
func finalizeSpeculativeOutput(res speculativeResult, isolation string) error {
	verifyStrictFailed := res.ar.verify.IsStrictFailure()
	hasPatchApplyErr := res.ar.patchApplyErr != nil

	if delegateFormat == "json" {
		dr := buildDelegationResult(res.agentKey, "", isolation, res.ar)
		data, _ := json.MarshalIndent(dr, "", "  ")
		fmt.Println(string(data))
		if res.ar.err != nil || verifyStrictFailed || hasPatchApplyErr {
			return &agent.ExitStatusError{Code: 1}
		}
		return nil
	}

	// Text format
	if res.ar.err != nil {
		fmt.Fprintf(os.Stderr, "[quancode] delegation error: %v\n", res.ar.err)
		if res.ar.output != "" {
			fmt.Print(res.ar.output)
		}
		return &agent.ExitStatusError{Code: 1}
	}
	if hasPatchApplyErr {
		fmt.Fprintf(os.Stderr, "[quancode] patch apply failed: %v\n", res.ar.patchApplyErr)
		if res.ar.output != "" {
			fmt.Print(res.ar.output)
		}
		return &agent.ExitStatusError{Code: 1}
	}
	if verifyStrictFailed {
		fmt.Fprintf(os.Stderr, "[quancode] delegation failed: verify-strict failed\n")
		if res.ar.output != "" {
			fmt.Print(res.ar.output)
		}
		return &agent.ExitStatusError{Code: 1}
	}
	if isolation == "patch" && res.ar.patch != "" {
		fmt.Fprintf(os.Stderr, "[quancode] patch (%d files changed, not applied):\n", len(res.ar.patchFiles))
		fmt.Print(res.ar.patch)
		return nil
	}
	fmt.Print(res.ar.output)
	return nil
}

// buildContextPrefix builds the context prefix for a specific agent.
func buildContextPrefix(cfg *config.Config, agentKey string, opts speculativeDelegationOpts) string {
	if opts.noContext {
		return ""
	}
	ac := cfg.Agents[agentKey]
	builder := qcontext.NewBuilder(cfg.ContextDefaults, ac.Context)
	bundle := builder.Build(opts.workDir, opts.contextFiles, opts.contextDiff, opts.contextMaxSize)
	for _, w := range bundle.Warnings {
		fmt.Fprintf(os.Stderr, "[quancode] context: %s\n", w)
	}
	return qcontext.Format(bundle)
}
