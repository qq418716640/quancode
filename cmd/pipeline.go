package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/config"
	qcontext "github.com/qq418716640/quancode/context"
	"github.com/qq418716640/quancode/ledger"
	"github.com/qq418716640/quancode/router"
	"github.com/qq418716640/quancode/runner"
	"github.com/qq418716640/quancode/ui"
	"github.com/spf13/cobra"
)

var (
	pipelineFormat    string
	pipelineWorkdir   string
	pipelineIsolation string
	pipelineDryRun    bool
	pipelineNoContext bool
)

// PipelineResult is the JSON output for a pipeline run.
type PipelineResult struct {
	Pipeline      string              `json:"pipeline"`
	PipelineID    string              `json:"pipeline_id"`
	Status        string              `json:"status"`
	TotalDuration int64               `json:"total_duration_ms"`
	Stages        []PipelineStageJSON `json:"stages"`
	ChangedFiles  []string            `json:"changed_files,omitempty"`
	Patch         string              `json:"patch,omitempty"`
}

// PipelineStageJSON is the JSON representation of a single stage result.
type PipelineStageJSON struct {
	Name         string   `json:"name"`
	Agent        string   `json:"agent"`
	Status       string   `json:"status"`
	ExitCode     int      `json:"exit_code"`
	TimedOut     bool     `json:"timed_out,omitempty"`
	DurationMs   int64    `json:"duration_ms"`
	Output       string   `json:"output"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	Skipped      bool     `json:"skipped,omitempty"`
}

// stageResult holds the runtime result of a completed stage.
type stageResult struct {
	Name         string
	AgentKey     string
	Output       string
	ChangedFiles []string
	ExitCode     int
	TimedOut     bool
	DurationMs   int64
	FailureClass string
	Verify       *VerifyResult
	Skipped      bool
	// FallbackChain records the agents tried for this stage (including the final one).
	FallbackChain []ui.ChainLink
}

// pipelineContext is the template data available to stage task templates.
type pipelineContext struct {
	Input  string
	Prev   *stageResult
	Stages map[string]*stageResult
}

var pipelineCmd = &cobra.Command{
	Use:   "pipeline <name-or-file> [task description]",
	Short: "Run a multi-stage delegation pipeline",
	Long:  "Executes an ordered sequence of delegation stages defined in a YAML file. Each stage's output can flow into the next via template variables.",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return err
		}

		pipelineName := args[0]
		input := ""
		if len(args) > 1 {
			input = strings.Join(args[1:], " ")
		}

		def, err := config.LoadPipeline(pipelineName)
		if err != nil {
			return err
		}

		if problems := def.Validate(cfg); len(problems) > 0 {
			for _, p := range problems {
				fmt.Fprintf(os.Stderr, "[quancode] pipeline error: %s\n", p)
			}
			return fmt.Errorf("pipeline validation failed (%d errors)", len(problems))
		}

		// Validate template references (stage ordering)
		if refProblems := validateTemplateRefs(def); len(refProblems) > 0 {
			for _, p := range refProblems {
				fmt.Fprintf(os.Stderr, "[quancode] pipeline error: %s\n", p)
			}
			return fmt.Errorf("pipeline template validation failed (%d errors)", len(refProblems))
		}

		workDir := pipelineWorkdir
		if workDir == "" {
			workDir, _ = os.Getwd()
		}

		// Resolve isolation
		isolation := pipelineIsolation
		if isolation == "" {
			isolation = cfg.Preferences.DefaultIsolation
		}
		if isolation == "" {
			isolation = "worktree"
		}

		if pipelineDryRun {
			return dryRunPipeline(cfg, def, input, isolation)
		}

		return runPipeline(cfg, def, input, workDir, isolation)
	},
}

func dryRunPipeline(cfg *config.Config, def *config.PipelineDef, input, isolation string) error {
	fmt.Fprintf(os.Stderr, "[quancode] pipeline dry-run: %s (%d stages, isolation: %s)\n",
		def.Name, len(def.Stages), isolation)

	for i, s := range def.Stages {
		agentKey := s.Agent
		if agentKey == "" {
			agentKey = "(auto-route)"
		}
		fmt.Fprintf(os.Stderr, "[quancode] [%d/%d] %s → %s\n", i+1, len(def.Stages), s.Name, agentKey)

		// Try to render template with placeholder context (use zero for missing keys in dry-run)
		dryCtx := &pipelineContext{
			Input:  input,
			Prev:   &stageResult{},
			Stages: map[string]*stageResult{},
		}
		rendered, err := renderStageTaskDryRun(s.Task, dryCtx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[quancode]   task: [template error: %v]\n", err)
		} else {
			display := ui.FirstLine(rendered, 80)
			fmt.Fprintf(os.Stderr, "[quancode]   task: %s\n", display)
		}

		if len(s.Verify) > 0 {
			strict := ""
			if s.VerifyStrict {
				strict = " (strict)"
			}
			fmt.Fprintf(os.Stderr, "[quancode]   verify%s: %s\n", strict, strings.Join(s.Verify, "; "))
		}

		onFail := def.ResolveOnFailure(s)
		if onFail != "stop" {
			fmt.Fprintf(os.Stderr, "[quancode]   on_failure: %s\n", onFail)
		}
	}
	return nil
}

func runPipeline(cfg *config.Config, def *config.PipelineDef, input, workDir, isolation string) error {
	pipelineID, err := ledger.NewPipelineID()
	if err != nil {
		return fmt.Errorf("generate pipeline id: %w", err)
	}

	totalStages := len(def.Stages)
	fmt.Fprintf(os.Stderr, "[quancode] pipeline: %s (%d stages)\n", def.Name, totalStages)

	// Set up execution directory
	execDir := workDir
	var baseSHA string
	var cleanupWorktree func()

	if isolation == "worktree" || isolation == "patch" {
		if !runner.IsGitRepo(workDir) {
			return fmt.Errorf("pipeline with isolation %s requires a git repository", isolation)
		}

		// Record base SHA for CollectPatchSince
		shaCmd := exec.Command("git", "rev-parse", "HEAD")
		shaCmd.Dir = workDir
		shaOut, shaErr := shaCmd.Output()
		if shaErr != nil {
			return fmt.Errorf("git rev-parse HEAD: %w", shaErr)
		}
		baseSHA = strings.TrimSpace(string(shaOut))

		wt, cleanup, wtErr := runner.CreateWorktree(workDir)
		if wtErr != nil {
			return fmt.Errorf("create pipeline worktree: %w", wtErr)
		}
		cleanupWorktree = cleanup
		defer func() {
			if cleanupWorktree != nil {
				cleanupWorktree()
			}
		}()
		execDir = wt
		fmt.Fprintf(os.Stderr, "[quancode] pipeline worktree: %s\n", wt)
	} else {
		fmt.Fprintf(os.Stderr, "[quancode] warning: pipeline running in inplace mode — failed stages cannot be rolled back\n")
	}

	// Execute stages
	pctx := &pipelineContext{
		Input:  input,
		Stages: make(map[string]*stageResult),
	}

	startTime := time.Now()
	var results []stageResult
	var allChangedFiles []string
	pipelineSuccess := true
	completedCount := 0

	for i, stageDef := range def.Stages {
		// Render task template
		rendered, tmplErr := renderStageTask(stageDef.Task, pctx)
		if tmplErr != nil {
			fmt.Fprintf(os.Stderr, "[quancode] [%d/%d] %s — template error: %v\n", i+1, totalStages, stageDef.Name, tmplErr)
			pipelineSuccess = false
			results = append(results, stageResult{Name: stageDef.Name, ExitCode: 1, FailureClass: FailureClassTemplateError})
			if def.ResolveOnFailure(stageDef) == "stop" {
				break
			}
			fmt.Fprintf(os.Stderr, "[quancode]   continuing (on_failure: continue)\n")
			continue
		}

		// Resolve agent
		agentKey := stageDef.Agent
		if agentKey == "" {
			sel := router.SelectAgent(cfg, rendered)
			if sel != nil {
				agentKey = sel.AgentKey
			}
		}
		if agentKey == "" {
			fmt.Fprintf(os.Stderr, "[quancode] [%d/%d] %s — no agent available\n", i+1, totalStages, stageDef.Name)
			pipelineSuccess = false
			results = append(results, stageResult{Name: stageDef.Name, ExitCode: 1, FailureClass: FailureClassLaunchFailure})
			if def.ResolveOnFailure(stageDef) == "stop" {
				break
			}
			continue
		}

		ac := cfg.Agents[agentKey]
		a := agent.FromConfig(agentKey, ac)
		if avail, _ := a.IsAvailable(); !avail {
			fmt.Fprintf(os.Stderr, "[quancode] [%d/%d] %s — agent %s not available\n", i+1, totalStages, stageDef.Name, agentKey)
			pipelineSuccess = false
			results = append(results, stageResult{Name: stageDef.Name, AgentKey: agentKey, ExitCode: 1, FailureClass: FailureClassLaunchFailure})
			if def.ResolveOnFailure(stageDef) == "stop" {
				break
			}
			continue
		}

		// Build verify spec
		var vs *verifySpec
		if len(stageDef.Verify) > 0 {
			vs = &verifySpec{
				Commands:   stageDef.Verify,
				Strict:     stageDef.VerifyStrict,
				TimeoutSec: 120,
			}
		}

		fmt.Fprintf(os.Stderr, "[quancode] [%d/%d] %s → %s\n", i+1, totalStages, stageDef.Name, agentKey)

		// Checkpoint worktree state so we can restore on fallback
		checkpointErr := checkpointWorktree(execDir)
		if checkpointErr != nil {
			fmt.Fprintf(os.Stderr, "[quancode] warning: checkpoint failed: %v\n", checkpointErr)
		}

		// Stage execution with fallback loop
		fl := newFallbackLoop(cfg, rendered, agentKey, 0)
		currentAgentKey := agentKey
		currentAgent := a
		var ar attemptResult
		var chain []ui.ChainLink
		attempt := 1

		runID, runIDErr := ledger.NewRunID()
		if runIDErr != nil {
			fmt.Fprintf(os.Stderr, "[quancode] warning: generate run id: %v\n", runIDErr)
		}

		for {
			// Build context per-agent (may differ between agents)
			var attemptCtxPrefix string
			if !pipelineNoContext {
				currentAc := cfg.Agents[currentAgentKey]
				builder := qcontext.NewBuilder(cfg.ContextDefaults, currentAc.Context)
				bundle := builder.Build(execDir, nil, "", 0)
				attemptCtxPrefix = qcontext.Format(bundle)
				for _, w := range bundle.Warnings {
					fmt.Fprintf(os.Stderr, "[quancode] context: %s\n", w)
				}
			}

			ar = runDelegateAttempt(DelegateAttemptOptions{
				Agent:           currentAgent,
				AgentKey:        currentAgentKey,
				Task:            rendered,
				CtxPrefix:       attemptCtxPrefix,
				WorkDir:         execDir,
				Isolation:       "inplace",
				Verify:          vs,
				TimeoutOverride: stageDef.TimeoutSecs,
			})

			// Log each attempt to ledger
			meta := attemptMeta{RunID: runID, Attempt: attempt}
			if attempt > 1 {
				meta.FallbackFrom = chain[len(chain)-1].Agent
				meta.FallbackReason = chain[len(chain)-1].FailureClass
			}
			logPipelineEntry(pipelineID, def.Name, stageDef.Name, i, currentAgentKey, rendered, execDir, meta, ar)

			if !fl.shouldRetry(ar, attempt) {
				break
			}

			chain = append(chain, ui.ChainLink{Agent: currentAgentKey, FailureClass: ar.failureClass})
			fmt.Fprintf(os.Stderr, "[quancode]   %s %s, looking for fallback...\n", currentAgentKey, ar.failureClass)

			nextKey, nextA, nextReason := fl.nextAgent()
			if nextA == nil {
				fmt.Fprintf(os.Stderr, "[quancode]   no fallback agents available\n")
				break
			}

			// Restore worktree to checkpoint before retrying
			if restoreErr := restoreCheckpoint(execDir); restoreErr != nil {
				fmt.Fprintf(os.Stderr, "[quancode] warning: restore failed: %v\n", restoreErr)
			}

			fmt.Fprintf(os.Stderr, "[quancode]   falling back to %s (%s)\n", nextKey, nextReason)
			currentAgentKey = nextKey
			currentAgent = nextA
			attempt++
		}

		// Build stage result from final attempt
		sr := stageResult{
			Name:     stageDef.Name,
			AgentKey: currentAgentKey,
		}
		if ar.result != nil {
			sr.Output = ar.output
			sr.ChangedFiles = ar.changedFiles
			sr.ExitCode = ar.result.ExitCode
			sr.TimedOut = ar.result.TimedOut
			sr.DurationMs = ar.result.DurationMs
		}
		sr.FailureClass = ar.failureClass
		sr.Verify = ar.verify
		if len(chain) > 0 {
			chain = append(chain, ui.ChainLink{Agent: currentAgentKey, FailureClass: ar.failureClass})
			sr.FallbackChain = chain
		}

		results = append(results, sr)
		pctx.Stages[stageDef.Name] = &results[len(results)-1]
		pctx.Prev = pctx.Stages[stageDef.Name]

		allChangedFiles = appendUnique(allChangedFiles, sr.ChangedFiles)

		// Check outcome
		if sr.FailureClass != "" {
			pipelineSuccess = false
			fmt.Fprintf(os.Stderr, "[quancode]   ✗ %s failed (%s)\n", stageDef.Name, sr.FailureClass)
			if len(sr.FallbackChain) > 0 {
				ui.FallbackChain(sr.FallbackChain)
			}

			if def.ResolveOnFailure(stageDef) == "stop" {
				break
			}
			fmt.Fprintf(os.Stderr, "[quancode]   continuing (on_failure: continue)\n")
		} else {
			completedCount++
			dur := ui.FormatDuration(sr.DurationMs)
			if len(sr.FallbackChain) > 0 {
				ui.FallbackChain(sr.FallbackChain)
			}
			if len(sr.ChangedFiles) > 0 {
				fmt.Fprintf(os.Stderr, "[quancode]   ✓ completed in %s — %d file(s) changed\n", dur, len(sr.ChangedFiles))
			} else {
				fmt.Fprintf(os.Stderr, "[quancode]   ✓ completed in %s\n", dur)
			}
		}
	}

	totalDuration := time.Since(startTime).Milliseconds()

	// Collect and apply pipeline patch (only if pipeline succeeded)
	var totalPatch string
	var patchFiles []string
	if isolation == "worktree" || isolation == "patch" {
		var collectErr error
		totalPatch, patchFiles, collectErr = runner.CollectPatchSince(execDir, baseSHA)
		if collectErr != nil {
			fmt.Fprintf(os.Stderr, "[quancode] warning: patch collection failed: %v\n", collectErr)
		}

		if !pipelineSuccess {
			// Don't apply partial results from a failed pipeline
			if totalPatch != "" {
				fmt.Fprintf(os.Stderr, "[quancode] pipeline failed — patch NOT applied (%d files changed in worktree)\n", len(patchFiles))
			}
		} else if isolation == "worktree" && totalPatch != "" {
			conflicts := runner.CheckPatchConflicts(workDir, totalPatch)
			if len(conflicts) > 0 {
				pipelineSuccess = false
				fmt.Fprintf(os.Stderr, "[quancode] pipeline patch conflicts with %d file(s):\n", len(conflicts))
				for _, f := range conflicts {
					fmt.Fprintf(os.Stderr, "[quancode]   - %s\n", f)
				}
			} else if applyErr := runner.ApplyPatch(workDir, totalPatch); applyErr != nil {
				pipelineSuccess = false
				fmt.Fprintf(os.Stderr, "[quancode] pipeline patch apply failed: %v\n", applyErr)
			} else {
				fmt.Fprintf(os.Stderr, "[quancode] pipeline changes applied to working directory\n")
			}
		}

		// Use patch files as the canonical changed files list
		if len(patchFiles) > 0 {
			allChangedFiles = patchFiles
		}
	}

	// Pipeline-level verification (runs after all stages, in the final directory)
	if pipelineSuccess && len(def.Verify) > 0 {
		verifyDir := workDir
		if isolation == "inplace" {
			verifyDir = execDir
		}
		pipelineVS := &verifySpec{
			Commands:   def.Verify,
			Strict:     def.VerifyStrict,
			TimeoutSec: 120,
		}
		vr := runAndLogVerification(verifyDir, pipelineVS)
		if vr.IsStrictFailure() {
			pipelineSuccess = false
		}
	}

	// Pipeline summary
	status := StatusCompleted
	if !pipelineSuccess {
		status = StatusFailed
	}

	totalDur := ui.FormatDuration(totalDuration)
	if pipelineSuccess {
		fmt.Fprintf(os.Stderr, "[quancode] ✓ pipeline completed: %d/%d stages (%s, %d files changed)\n",
			completedCount, len(def.Stages), totalDur, len(allChangedFiles))
	} else {
		fmt.Fprintf(os.Stderr, "[quancode] ✗ pipeline failed: %d/%d stages completed (%s)\n",
			completedCount, len(def.Stages), totalDur)
	}

	// Output
	if pipelineFormat == "json" {
		pr := PipelineResult{
			Pipeline:      def.Name,
			PipelineID:    pipelineID,
			Status:        status,
			TotalDuration: totalDuration,
			ChangedFiles:  allChangedFiles,
		}
		if isolation == "patch" && totalPatch != "" {
			pr.Patch = totalPatch
		}
		for _, sr := range results {
			sj := PipelineStageJSON{
				Name:         sr.Name,
				Agent:        sr.AgentKey,
				Status:       stageStatus(sr),
				ExitCode:     sr.ExitCode,
				TimedOut:     sr.TimedOut,
				DurationMs:   sr.DurationMs,
				Output:       sr.Output,
				ChangedFiles: sr.ChangedFiles,
				Skipped:      sr.Skipped,
			}
			pr.Stages = append(pr.Stages, sj)
		}
		data, _ := json.MarshalIndent(pr, "", "  ")
		fmt.Println(string(data))
	} else {
		// Text format: print last stage output for piping
		if isolation == "patch" && totalPatch != "" {
			fmt.Fprintf(os.Stderr, "[quancode] patch (%d files changed, not applied):\n", len(patchFiles))
			fmt.Print(totalPatch)
		} else if len(results) > 0 {
			last := results[len(results)-1]
			if last.Output != "" {
				fmt.Print(last.Output)
			}
		}
	}

	if !pipelineSuccess {
		return &agent.ExitStatusError{Code: 1}
	}
	return nil
}

// checkpointWorktree creates a temporary commit to snapshot the current state.
// This allows restoreCheckpoint to revert failed stage attempts.
func checkpointWorktree(dir string) error {
	add := exec.Command("git", "add", "-A")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %s: %w", string(out), err)
	}
	commit := exec.Command("git", "commit", "--allow-empty", "-m", "quancode: stage checkpoint")
	commit.Dir = dir
	if out, err := commit.CombinedOutput(); err != nil {
		// "nothing to commit" is fine
		if !strings.Contains(string(out), "nothing to commit") {
			return fmt.Errorf("git commit: %s: %w", string(out), err)
		}
	}
	return nil
}

// restoreCheckpoint reverts the worktree to the last checkpoint commit.
func restoreCheckpoint(dir string) error {
	reset := exec.Command("git", "reset", "--hard", "HEAD")
	reset.Dir = dir
	if out, err := reset.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset: %s: %w", string(out), err)
	}
	clean := exec.Command("git", "clean", "-fd")
	clean.Dir = dir
	if out, err := clean.CombinedOutput(); err != nil {
		return fmt.Errorf("git clean: %s: %w", string(out), err)
	}
	return nil
}

// validateTemplateRefs checks that stage task templates only reference
// stages declared before them. Uses the Go template engine with
// missingkey=error to detect invalid references.
func validateTemplateRefs(def *config.PipelineDef) []string {
	var problems []string
	pctx := &pipelineContext{
		Input:  "placeholder",
		Stages: make(map[string]*stageResult),
	}

	for _, s := range def.Stages {
		if s.Task == "" {
			continue
		}
		_, err := renderStageTask(s.Task, pctx)
		if err != nil {
			problems = append(problems, fmt.Sprintf("stage %q: %v", s.Name, err))
		}
		// Register this stage so subsequent stages can reference it
		pctx.Stages[s.Name] = &stageResult{}
		pctx.Prev = pctx.Stages[s.Name]
	}
	return problems
}

func renderStageTask(tmplStr string, pctx *pipelineContext) (string, error) {
	return renderTemplate(tmplStr, pctx, "missingkey=error")
}

func renderStageTaskDryRun(tmplStr string, pctx *pipelineContext) (string, error) {
	return renderTemplate(tmplStr, pctx, "missingkey=zero")
}

func renderTemplate(tmplStr string, pctx *pipelineContext, missingKeyOpt string) (string, error) {
	tmpl, err := template.New("stage").Option(missingKeyOpt).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, pctx); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

func logPipelineEntry(pipelineID, pipelineName, stageName string, stageIndex int,
	agentKey, task, workDir string, meta attemptMeta, ar attemptResult) {
	logEntry := &ledger.Entry{
		Agent:          agentKey,
		Task:           task,
		WorkDir:        workDir,
		Isolation:      "inplace",
		RunID:          meta.RunID,
		Attempt:        meta.Attempt,
		FallbackFrom:   meta.FallbackFrom,
		FallbackReason: meta.FallbackReason,
		PipelineID:     pipelineID,
		PipelineName:   pipelineName,
		StageName:      stageName,
		StageIndex:     stageIndex,
	}
	if ar.result != nil {
		logEntry.ExitCode = ar.result.ExitCode
		logEntry.TimedOut = ar.result.TimedOut
		logEntry.DurationMs = ar.result.DurationMs
		logEntry.ChangedFiles = ar.changedFiles
	}
	logEntry.FailureClass = ar.failureClass
	if (ar.err != nil) && logEntry.ExitCode == 0 {
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

func stageStatus(sr stageResult) string {
	if sr.Skipped {
		return StatusSkipped
	}
	return determineFinalStatus(sr.ExitCode, sr.TimedOut, sr.Verify)
}

func appendUnique(base, items []string) []string {
	seen := make(map[string]bool, len(base))
	for _, s := range base {
		seen[s] = true
	}
	for _, s := range items {
		if !seen[s] {
			seen[s] = true
			base = append(base, s)
		}
	}
	return base
}

func init() {
	pipelineCmd.Flags().StringVar(&pipelineFormat, "format", "text", "output format: text or json")
	pipelineCmd.Flags().StringVar(&pipelineWorkdir, "workdir", "", "working directory (default: current)")
	pipelineCmd.Flags().StringVar(&pipelineIsolation, "isolation", "", "isolation mode: worktree, patch, or inplace (default: worktree)")
	pipelineCmd.Flags().BoolVar(&pipelineDryRun, "dry-run", false, "show execution plan without running")
	pipelineCmd.Flags().BoolVar(&pipelineNoContext, "no-context", false, "disable automatic context injection")
	rootCmd.AddCommand(pipelineCmd)
}
