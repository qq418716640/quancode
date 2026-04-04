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
			ContextDiffMode: opts.contextDiff,
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
			return finalizeSpeculativeCollected(opts, &res, nil, runID)
		}
		if !isTransientFailure(res.ar.failureClass) {
			// Non-transient failure (agent_failed, etc.) — no fallback
			return finalizeSpeculativeCollected(opts, &res, nil, runID)
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
				ContextDiffMode: opts.contextDiff,
			})
			resultCh <- speculativeResult{agentKey: specSel.AgentKey, ar: ar, role: "speculative"}
		}()
		specRes := <-resultCh
		spinner.Stop()
		spec := &specRes
		return finalizeSpeculativeCollected(opts, primary, spec, runID)

	case <-timer.C:
		// Window expired, primary still running — launch speculative
		spinner.Stop()
		fmt.Fprintf(os.Stderr, "[quancode] %s still running after %ds, launching %s in parallel...\n",
			opts.primaryKey, opts.delaySecs, specSel.AgentKey)
		spinner = ui.NewSpinner(fmt.Sprintf("%s + %s running in parallel...", opts.primaryKey, specSel.AgentKey))
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
			ContextDiffMode: opts.contextDiff,
		})
		resultCh <- speculativeResult{agentKey: specSel.AgentKey, ar: ar, role: "speculative"}
	}()

	// Wait for both results — no early cancellation
	var primary, spec *speculativeResult
	for i := 0; i < 2; i++ {
		res := <-resultCh
		if res.role == "primary" {
			primary = &res
		} else {
			spec = &res
		}
		// Show progress for first completion
		if i == 0 {
			spinner.Stop()
			status := "succeeded"
			if res.ar.failureClass != "" || res.ar.err != nil {
				status = "failed"
			}
			remaining := specSel.AgentKey
			if res.role == "speculative" {
				remaining = opts.primaryKey
			}
			fmt.Fprintf(os.Stderr, "[quancode] %s %s, waiting for %s...\n", res.agentKey, status, remaining)
			spinner = ui.NewSpinner(fmt.Sprintf("waiting for %s...", remaining))
		}
	}
	spinner.Stop()

	return finalizeSpeculativeCollected(opts, primary, spec, runID)
}

// finalizeSpeculativeCollected handles the case where both agents completed.
// It selects the preferred result (primary if successful), applies patch/verify,
// logs both entries, and outputs the results.
func finalizeSpeculativeCollected(opts speculativeDelegationOpts, primary, spec *speculativeResult, runID string) error {
	primaryOk := primary != nil && primary.ar.failureClass == "" && primary.ar.err == nil
	specOk := spec != nil && spec.ar.failureClass == "" && spec.ar.err == nil

	// Select the result to apply
	var selected, companion *speculativeResult
	var selectionReason string
	if primaryOk {
		selected, companion = primary, spec
		selectionReason = "primary_preferred"
	} else if specOk {
		selected, companion = spec, primary
		selectionReason = "primary_failed"
	} else {
		// Both failed — log both, return the one with more useful output
		return finalizeSpeculativeBothFailed(opts, primary, spec, runID)
	}

	// Apply patch from selected result only
	if opts.isolation == "worktree" && selected.ar.patch != "" {
		conflicts := runner.CheckPatchConflicts(opts.workDir, selected.ar.patch)
		if len(conflicts) > 0 {
			selected.ar.patchApplyErr = fmt.Errorf("patch conflicts with %d files", len(conflicts))
			selected.ar.conflictFiles = conflicts
		} else if applyErr := runner.ApplyPatch(opts.workDir, selected.ar.patch); applyErr != nil {
			selected.ar.patchApplyErr = applyErr
		} else {
			fmt.Fprintf(os.Stderr, "[quancode] changes from %s applied to working directory\n", selected.agentKey)
		}
	}

	// Run verification on selected result only
	if selected.ar.failureClass == "" && selected.ar.err == nil && opts.verify != nil && opts.isolation != "patch" {
		selected.ar.verify = runAndLogVerification(opts.workDir, opts.verify)
	}
	selected.ar.failureClass = classifyFailure(selected.ar)

	// Log selected entry
	selectedMeta := attemptMeta{RunID: runID, Attempt: 1}
	logSpeculativeEntry(selected.agentKey, opts.task, opts.workDir, opts.isolation,
		selectedMeta, selected.ar, selected.role, true, selectionReason)

	// Log companion entry
	if companion != nil {
		companionMeta := attemptMeta{RunID: runID, Attempt: 2}
		logSpeculativeEntry(companion.agentKey, opts.task, opts.workDir, opts.isolation,
			companionMeta, companion.ar, companion.role, false, selectionReason)
	}

	// UI
	if selected.ar.failureClass == "" && selected.ar.err == nil && selected.ar.patchApplyErr == nil {
		var durationMs int64
		if selected.ar.result != nil {
			durationMs = selected.ar.result.DurationMs
		}
		ui.DelegationSuccess(selected.agentKey, durationMs, len(selected.ar.changedFiles))
	}

	// Show summary
	if companion != nil {
		fmt.Fprintf(os.Stderr, "[quancode] Speculative: %s + %s → selected %s (%s)\n",
			primary.agentKey, spec.agentKey, selected.agentKey, selectionReason)
	}

	return finalizeSpeculativeOutput(*selected, companion, opts.isolation, selectionReason)
}

// finalizeSpeculativeBothFailed handles the case where both agents failed.
func finalizeSpeculativeBothFailed(opts speculativeDelegationOpts, primary, spec *speculativeResult, runID string) error {
	if primary != nil {
		meta := attemptMeta{RunID: runID, Attempt: 1}
		logSpeculativeEntry(primary.agentKey, opts.task, opts.workDir, opts.isolation,
			meta, primary.ar, "primary", false, "")
	}
	if spec != nil {
		meta := attemptMeta{RunID: runID, Attempt: 2}
		logSpeculativeEntry(spec.agentKey, opts.task, opts.workDir, opts.isolation,
			meta, spec.ar, "speculative", false, "")
	}

	// Use whichever has more useful output
	final := spec
	if final == nil {
		final = primary
	}

	if final.ar.result != nil {
		ui.DelegationFailure(final.agentKey, final.ar.result.DurationMs, final.ar.failureClass)
	}

	return finalizeSpeculativeOutput(*final, nil, opts.isolation, "")
}

// logSpeculativeEntry writes a ledger entry with speculative tracking fields.
func logSpeculativeEntry(agentKey, task, workDir, isolation string, meta attemptMeta, ar attemptResult, role string, selected bool, selectionReason string) {
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
		Selected:        selected,
		SelectionReason: selectionReason,
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
		logEntry.ChangedFiles = nil
	}
	if (ar.err != nil || ar.patchApplyErr != nil) && logEntry.ExitCode == 0 {
		logEntry.ExitCode = 1
	}
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

// finalizeSpeculativeOutput handles the output formatting for speculative delegation.
// companion may be nil when only one agent ran or both failed.
func finalizeSpeculativeOutput(selected speculativeResult, companion *speculativeResult, isolation, selectionReason string) error {
	verifyStrictFailed := selected.ar.verify.IsStrictFailure()
	hasPatchApplyErr := selected.ar.patchApplyErr != nil

	if delegateFormat == "json" {
		dr := buildDelegationResult(selected.agentKey, "", isolation, selected.ar)
		// Attach companion info when available
		if companion != nil {
			cdr := buildDelegationResult(companion.agentKey, "", isolation, companion.ar)
			cr := &CompanionResult{
				Agent:        companion.agentKey,
				DelegationID: companion.ar.delegationID,
				Status:       cdr.Status,
				ExitCode:     cdr.ExitCode,
				TimedOut:     cdr.TimedOut,
				Output:       cdr.Output,
				DurationMs:   cdr.DurationMs,
				ChangedFiles: cdr.ChangedFiles,
				Patch:        cdr.Patch,
			}
			dr.Speculative = &SpeculativeInfo{
				Mode:            "collected",
				Selected:        selected.role,
				SelectionReason: selectionReason,
				Companion:       cr,
			}
		}
		data, _ := json.MarshalIndent(dr, "", "  ")
		fmt.Println(string(data))
		if selected.ar.err != nil || verifyStrictFailed || hasPatchApplyErr {
			return &agent.ExitStatusError{Code: 1}
		}
		return nil
	}

	// Text format — stdout only has selected output; companion info goes to stderr
	if selected.ar.err != nil {
		fmt.Fprintf(os.Stderr, "[quancode] delegation error: %v\n", selected.ar.err)
		if selected.ar.output != "" {
			fmt.Print(selected.ar.output)
		}
		return &agent.ExitStatusError{Code: 1}
	}
	if hasPatchApplyErr {
		fmt.Fprintf(os.Stderr, "[quancode] patch apply failed: %v\n", selected.ar.patchApplyErr)
		if selected.ar.output != "" {
			fmt.Print(selected.ar.output)
		}
		return &agent.ExitStatusError{Code: 1}
	}
	if verifyStrictFailed {
		fmt.Fprintf(os.Stderr, "[quancode] delegation failed: verify-strict failed\n")
		if selected.ar.output != "" {
			fmt.Print(selected.ar.output)
		}
		return &agent.ExitStatusError{Code: 1}
	}
	if isolation == "patch" && selected.ar.patch != "" {
		fmt.Fprintf(os.Stderr, "[quancode] patch (%d files changed, not applied):\n", len(selected.ar.patchFiles))
		fmt.Print(selected.ar.patch)
		return nil
	}
	// Print selected output to stdout
	fmt.Print(selected.ar.output)
	// Note companion availability on stderr
	if companion != nil && companion.ar.output != "" && companion.ar.failureClass == "" {
		fmt.Fprintf(os.Stderr, "[quancode] companion output from %s available in ledger (delegation %s)\n",
			companion.agentKey, companion.ar.delegationID)
	}
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
