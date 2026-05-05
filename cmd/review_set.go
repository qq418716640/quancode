package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/config"
	qcontext "github.com/qq418716640/quancode/context"
	"github.com/qq418716640/quancode/ledger"
	"github.com/spf13/cobra"
)

var (
	reviewSetAgents         string
	reviewSetGroupID        string
	reviewSetWorkdir        string
	reviewSetFormat         string
	reviewSetIsolation      string
	reviewSetNoContext      bool
	reviewSetContextFiles   []string
	reviewSetContextDiff    string
	reviewSetContextMaxSize int
	reviewSetTimeout        int
	reviewSetHeadLines      int
)

// ReviewSetResult is the JSON shape returned for one `review-set` invocation.
// Each agent's per-attempt output appears in Results, ordered to match Agents.
type ReviewSetResult struct {
	ReviewSetID string             `json:"review_set_id"`
	Task        string             `json:"task"`
	Agents      []string           `json:"agents"`
	Isolation   string             `json:"isolation"`
	WorkDir     string             `json:"work_dir"`
	DurationMs  int64              `json:"duration_ms"`
	Results     []DelegationResult `json:"results"`
	Failed      int                `json:"failed"`
}

var reviewSetCmd = &cobra.Command{
	Use:   "review-set [task description]",
	Short: "Fan out the same task to multiple agents in parallel for human comparison",
	Long: `review-set runs the same task on multiple agents concurrently and aggregates their outputs.

All sibling delegations share a single review_set_id, so the dashboard can group
them; each agent still gets its own delegation_id and run_id. No cross-agent
fallback, no speculative parallelism, no verification — review-set is a fan-out
collector, not a competition.

Isolation notes:
  - 'inplace' (default): assumes a read-only task. Concurrent file writes by
    different agents will produce confusing changed_files attribution.
  - 'worktree' / 'patch': each agent runs in its own worktree. Patches are
    NEVER auto-applied (review-set always defers patch apply to avoid
    multi-agent merge conflicts in the main tree).

Example:
  quancode review-set --agents codex,qoder,gemini "review the diff in cmd/foo.go"`,
	Args: cobra.MinimumNArgs(1),
	RunE: runReviewSet,
}

// validReviewSetFormats restricts --format to known values; matches delegate.
var validReviewSetFormats = map[string]bool{"text": true, "json": true}

func runReviewSet(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	task := strings.Join(args, " ")

	if !validReviewSetFormats[reviewSetFormat] {
		return fmt.Errorf("--format must be 'text' or 'json' (got %q)", reviewSetFormat)
	}

	workDir := reviewSetWorkdir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	if reviewSetTimeout < 0 {
		return fmt.Errorf("--timeout must be a positive number of seconds")
	}

	agentKeys, err := resolveReviewSetAgents(cfg, reviewSetAgents)
	if err != nil {
		return err
	}
	if len(agentKeys) < 2 {
		return fmt.Errorf("review-set requires at least 2 agents (got %d)", len(agentKeys))
	}

	defaultIso := reviewSetIsolation
	if defaultIso == "" {
		defaultIso = cfg.Preferences.DefaultIsolation
	}
	if defaultIso == "" {
		defaultIso = "inplace"
	}

	reviewSetID := strings.TrimSpace(reviewSetGroupID)
	if reviewSetID == "" {
		reviewSetID, err = ledger.NewReviewSetID()
		if err != nil {
			return fmt.Errorf("generate review-set id: %w", err)
		}
	}

	plans, err := buildReviewSetPlans(cfg, agentKeys, defaultIso)
	if err != nil {
		return err
	}

	// Forward Ctrl+C / SIGTERM to all in-flight agent goroutines so the user
	// can interrupt a slow review-set without leaving subprocess orphans.
	ctx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			fmt.Fprintln(os.Stderr, "\n[quancode] interrupt received, cancelling all agents...")
			cancelAll()
		case <-ctx.Done():
		}
	}()

	fmt.Fprintf(os.Stderr, "[quancode] ⚡ review-set %s: dispatching to %d agents (%s) in parallel...\n",
		reviewSetID, len(plans), strings.Join(agentKeys, ", "))

	type fanoutResult struct {
		index int
		ar    attemptResult
	}
	resultCh := make(chan fanoutResult, len(plans))
	startTime := time.Now()

	for i, p := range plans {
		i, p := i, p
		go func() {
			var ctxPrefix string
			if !reviewSetNoContext {
				builder := qcontext.NewBuilder(cfg.ContextDefaults, p.ac.Context)
				bundle := builder.Build(workDir, reviewSetContextFiles, reviewSetContextDiff, reviewSetContextMaxSize)
				ctxPrefix = qcontext.Format(bundle)
				for _, w := range bundle.Warnings {
					fmt.Fprintf(os.Stderr, "[quancode][%s] context: %s\n", p.key, w)
				}
			}

			// review-set never auto-applies patches: N agents apply concurrently
			// would produce merge chaos in the main tree. Patch is preserved in
			// attemptResult and surfaced via JSON output / dashboard for inspection.
			deferApply := p.isolation == "worktree" || p.isolation == "patch"

			ar := runDelegateAttempt(DelegateAttemptOptions{
				Agent:                 p.agentImpl,
				AgentKey:              p.key,
				Task:                  task,
				CtxPrefix:             ctxPrefix,
				WorkDir:               workDir,
				Isolation:             p.isolation,
				TimeoutOverride:       reviewSetTimeout,
				MinTimeout:            cfg.Preferences.MinTimeoutSecs,
				ContextDiffMode:       reviewSetContextDiff,
				Quiet:                 true, // suppress per-agent spinner; we manage UI ourselves
				Ctx:                   ctx,
				DeferPatchApply:       deferApply,
				TaskSizeWarnThreshold: cfg.Preferences.EffectiveTaskSizeWarnThreshold(),
			})

			resultCh <- fanoutResult{index: i, ar: ar}
		}()
	}

	results := make([]attemptResult, len(plans))
	for range plans {
		r := <-resultCh
		results[r.index] = r.ar
	}
	totalDuration := time.Since(startTime).Milliseconds()

	rsResult := ReviewSetResult{
		ReviewSetID: reviewSetID,
		Task:        task,
		Agents:      agentKeys,
		Isolation:   defaultIso,
		WorkDir:     workDir,
		DurationMs:  totalDuration,
		Results:     make([]DelegationResult, 0, len(plans)),
	}

	for i, p := range plans {
		ar := results[i]
		runID, ridErr := ledger.NewRunID()
		if ridErr != nil {
			fmt.Fprintf(os.Stderr, "[quancode] warning: generate run id: %v\n", ridErr)
		}
		meta := attemptMeta{
			RunID:       runID,
			Attempt:     1,
			ReviewSetID: reviewSetID,
		}
		logAttempt(p.key, task, workDir, p.isolation, meta, ar)

		dr := buildDelegationResult(p.key, task, p.isolation, ar)
		rsResult.Results = append(rsResult.Results, dr)
		if dr.Status != StatusCompleted {
			rsResult.Failed++
		}
	}

	if reviewSetFormat == "json" {
		data, _ := json.MarshalIndent(rsResult, "", "  ")
		fmt.Println(string(data))
	} else {
		emitReviewSetText(rsResult, totalDuration)
	}

	if rsResult.Failed > 0 {
		return &agent.ExitStatusError{Code: 1}
	}
	return nil
}

// reviewSetPlan is the per-agent execution descriptor.
type reviewSetPlan struct {
	key       string
	ac        config.AgentConfig
	agentImpl agent.Agent
	isolation string
}

// buildReviewSetPlans validates each agent and resolves its effective isolation.
// Fails fast if any agent is unknown / disabled / unavailable.
func buildReviewSetPlans(cfg *config.Config, keys []string, defaultIso string) ([]reviewSetPlan, error) {
	plans := make([]reviewSetPlan, 0, len(keys))
	for _, key := range keys {
		ac, ok := cfg.Agents[key]
		if !ok {
			return nil, fmt.Errorf("unknown agent: %s", key)
		}
		if !ac.Enabled {
			return nil, fmt.Errorf("agent %s is disabled (set enabled: true in quancode.yaml to opt in)", key)
		}
		a := agent.FromConfig(key, ac)
		if ok, _ := a.IsAvailable(); !ok {
			return nil, fmt.Errorf("agent %s: command %q not found in PATH", key, ac.Command)
		}
		iso := defaultIso
		if !ac.SupportsIsolation(iso) {
			fallback := ac.FallbackIsolation()
			fmt.Fprintf(os.Stderr, "[quancode] warning: %s does not support isolation %q, falling back to %s\n",
				key, iso, fallback)
			iso = fallback
		}
		plans = append(plans, reviewSetPlan{key: key, ac: ac, agentImpl: a, isolation: iso})
	}
	return plans, nil
}

// resolveReviewSetAgents parses the --agents csv. Empty value means
// "all enabled non-primary agents" sorted alphabetically. When csv is
// empty, default_primary must be set in config so we know which agent
// to exclude — otherwise the primary would silently fan-out to itself.
func resolveReviewSetAgents(cfg *config.Config, csv string) ([]string, error) {
	csv = strings.TrimSpace(csv)
	if csv != "" {
		parts := strings.Split(csv, ",")
		out := make([]string, 0, len(parts))
		seen := map[string]bool{}
		for _, p := range parts {
			key := strings.TrimSpace(p)
			if key == "" {
				continue
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, key)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("--agents had no usable entries")
		}
		return out, nil
	}
	if cfg.DefaultPrimary == "" {
		return nil, fmt.Errorf("default_primary not set in config; pass --agents to specify explicitly")
	}
	keys := make([]string, 0, len(cfg.Agents))
	for key, ac := range cfg.Agents {
		if !ac.Enabled || key == cfg.DefaultPrimary {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

// emitReviewSetText prints a human-readable summary: per-agent header + head
// of output, followed by an aggregated footer. Full outputs live in the
// ledger output files for later inspection.
func emitReviewSetText(rs ReviewSetResult, totalDuration int64) {
	for _, dr := range rs.Results {
		fmt.Println()
		fmt.Printf("=== %s (%s) — %s in %.1fs ===\n",
			dr.Agent, dr.DelegationID, dr.Status, float64(dr.DurationMs)/1000)
		if dr.Output != "" {
			fmt.Println(headLines(dr.Output, reviewSetHeadLines))
		}
	}
	fmt.Println()
	fmt.Fprintf(os.Stderr, "[quancode] review-set %s complete: %d succeeded, %d failed (total %.1fs)\n",
		rs.ReviewSetID, len(rs.Results)-rs.Failed, rs.Failed, float64(totalDuration)/1000)
	fmt.Fprintf(os.Stderr, "[quancode] full outputs: %s/<delegation_id>.output\n", ledger.OutputDir())
}

// headLines returns the first n lines of s with a truncation marker.
// Returns the full string when n <= 0 or s has fewer than n lines.
func headLines(s string, n int) string {
	if n <= 0 {
		return s
	}
	count := 0
	for i, r := range s {
		if r == '\n' {
			count++
			if count == n {
				return s[:i] + "\n... (truncated; full output in ledger)"
			}
		}
	}
	return s
}

func init() {
	reviewSetCmd.Flags().StringVar(&reviewSetAgents, "agents", "", "comma-separated agent keys (default: all enabled non-primary)")
	_ = reviewSetCmd.RegisterFlagCompletionFunc("agents", completeAgentKeys)
	reviewSetCmd.Flags().StringVar(&reviewSetGroupID, "group-id", "", "review-set id (default: auto-generated rs_<hex>)")
	reviewSetCmd.Flags().StringVar(&reviewSetWorkdir, "workdir", "", "working directory (default: current)")
	reviewSetCmd.Flags().StringVar(&reviewSetFormat, "format", "text", "output format: text or json")
	reviewSetCmd.Flags().StringVar(&reviewSetIsolation, "isolation", "", "isolation mode: inplace, worktree, patch (default: preferences)")
	reviewSetCmd.Flags().BoolVar(&reviewSetNoContext, "no-context", false, "disable automatic context injection")
	reviewSetCmd.Flags().StringArrayVar(&reviewSetContextFiles, "context-files", nil, "additional context files (can be specified multiple times)")
	reviewSetCmd.Flags().StringVar(&reviewSetContextDiff, "context-diff", "", "include git diff: staged, working, or empty")
	reviewSetCmd.Flags().IntVar(&reviewSetContextMaxSize, "context-max-size", 0, "override max context size in bytes (0 = config default)")
	reviewSetCmd.Flags().IntVar(&reviewSetTimeout, "timeout", 0, "per-agent timeout in seconds")
	reviewSetCmd.Flags().IntVar(&reviewSetHeadLines, "head", 50, "max lines per agent in text output (0 = no truncation)")
	rootCmd.AddCommand(reviewSetCmd)
}
