package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

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
	delegateAgent          string
	delegateWorkdir        string
	delegateFormat         string
	delegateIsolation      string
	delegateNoFallback     bool
	delegateContextFiles   []string
	delegateContextDiff    string
	delegateContextMaxSize int
	delegateNoContext      bool
	delegateDryRun         bool
	delegateVerify         []string
	delegateVerifyStrict   []string
	delegateVerifyTimeout  int
	delegateAsync          bool
	delegateTimeout        int
)

type dryRunResult struct {
	Agent     string      `json:"agent"`
	Task      string      `json:"task"`
	Isolation string      `json:"isolation"`
	WorkDir   string      `json:"work_dir"`
	Verify    *verifySpec `json:"verify,omitempty"`
}

type DelegationResult struct {
	Agent         string        `json:"agent"`
	Task          string        `json:"task"`
	DelegationID  string        `json:"delegation_id,omitempty"`
	Status        string        `json:"status,omitempty"`
	ExitCode      int           `json:"exit_code"`
	TimedOut      bool          `json:"timed_out"`
	DurationMs    int64         `json:"duration_ms"`
	Output        string        `json:"output"`
	ChangedFiles  []string      `json:"changed_files"`
	Isolation     string        `json:"isolation,omitempty"`
	Patch         string        `json:"patch,omitempty"`
	Verify        *VerifyResult `json:"verify,omitempty"`
	ConflictFiles []string      `json:"conflict_files,omitempty"`
}

func buildDelegationResult(agentKey, task, isolation string, ar attemptResult) DelegationResult {
	dr := DelegationResult{
		Agent:     agentKey,
		Task:      task,
		Isolation: isolation,
		Verify:    ar.verify,
	}
	if ar.result != nil {
		dr.DelegationID = ar.result.DelegationID
		dr.ExitCode = ar.result.ExitCode
		dr.TimedOut = ar.result.TimedOut
		dr.DurationMs = ar.result.DurationMs
		dr.Output = ar.output
		dr.ChangedFiles = ar.changedFiles
	}
	if isolation == "patch" && ar.patch != "" {
		dr.Patch = ar.patch
	}
	// Include patch content and conflict details when apply failed
	if ar.patchApplyErr != nil {
		dr.Patch = ar.patch
		dr.ConflictFiles = ar.conflictFiles
		if dr.ExitCode == 0 {
			dr.ExitCode = 1
		}
	}
	if ar.err != nil {
		dr.Output = ar.output
		if dr.ExitCode == 0 {
			dr.ExitCode = 1
		}
	}
	dr.Status = determineFinalStatus(dr.ExitCode, dr.TimedOut, ar.verify)
	return dr
}

var delegateCmd = &cobra.Command{
	Use:   "delegate [task description]",
	Short: "Delegate a task to a sub-agent CLI",
	Long:  "Runs a task on the specified AI coding CLI in non-interactive mode and returns the result.",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return err
		}

		task := strings.Join(args, " ")

		// Resolve working directory
		workDir := delegateWorkdir
		if workDir == "" {
			workDir, _ = os.Getwd()
		}

		// Resolve isolation: CLI flag > agent default > preferences > "inplace"
		isolation := delegateIsolation
		// Agent-level default is resolved after agent selection below

		if delegateTimeout < 0 {
			return fmt.Errorf("--timeout must be a positive number of seconds")
		}

		// Resolve initial agent
		agentKey := delegateAgent
		if agentKey == "" {
			sel := router.SelectAgent(cfg, task)
			if sel != nil {
				agentKey = sel.AgentKey
				fmt.Fprintf(os.Stderr, "[quancode] auto-routed to %s (%s)\n", agentKey, sel.Reason)
			}
		}
		if agentKey == "" {
			return fmt.Errorf("no agent specified and no available sub-agent found")
		}

		// Validate initial agent
		ac, ok := cfg.Agents[agentKey]
		if !ok {
			return fmt.Errorf("unknown agent: %s", agentKey)
		}
		if !ac.Enabled {
			return fmt.Errorf("agent %s is disabled", agentKey)
		}
		a := agent.FromConfig(agentKey, ac)
		if !delegateDryRun {
			if ok, _ := a.IsAvailable(); !ok {
				return fmt.Errorf("agent %s: command %q not found in PATH", agentKey, ac.Command)
			}
		}

		// Finish resolving isolation: CLI flag > agent default > preferences > "inplace"
		if isolation == "" {
			isolation = ac.DefaultIsolation
		}
		if isolation == "" {
			isolation = cfg.Preferences.DefaultIsolation
		}
		if isolation == "" {
			isolation = "inplace"
		}

		// Warn if the agent doesn't support the resolved isolation mode
		if !ac.SupportsIsolation(isolation) {
			fallbackIso := ac.FallbackIsolation()
			fmt.Fprintf(os.Stderr, "[quancode] warning: %s does not support isolation %q, falling back to %s\n",
				agentKey, isolation, fallbackIso)
			isolation = fallbackIso
		}

		// Validate verify flags (mutually exclusive)
		if len(delegateVerify) > 0 && len(delegateVerifyStrict) > 0 {
			return fmt.Errorf("--verify and --verify-strict are mutually exclusive")
		}
		var verifyCmds []string
		var verifyStrict bool
		if len(delegateVerifyStrict) > 0 {
			verifyCmds = delegateVerifyStrict
			verifyStrict = true
		} else if len(delegateVerify) > 0 {
			verifyCmds = delegateVerify
		}

		// Build verify spec
		var vs *verifySpec
		if len(verifyCmds) > 0 {
			vs = &verifySpec{
				Commands:   verifyCmds,
				Strict:     verifyStrict,
				TimeoutSec: delegateVerifyTimeout,
			}
		}

		// Dry-run: show what would be sent to the agent, then exit
		if delegateDryRun {
			var ctxPrefix string
			if !delegateNoContext {
				builder := qcontext.NewBuilder(cfg.ContextDefaults, ac.Context)
				bundle := builder.Build(workDir, delegateContextFiles, delegateContextDiff, delegateContextMaxSize)
				ctxPrefix = qcontext.Format(bundle)
				for _, w := range bundle.Warnings {
					fmt.Fprintf(os.Stderr, "[quancode] context: %s\n", w)
				}
			}
			fullTask := task
			if ctxPrefix != "" {
				fullTask = ctxPrefix + "\n\n=== TASK ===\n\n" + task
			}
			if delegateFormat == "json" {
				data, _ := json.MarshalIndent(dryRunResult{
					Agent:     agentKey,
					Task:      fullTask,
					Isolation: isolation,
					WorkDir:   workDir,
					Verify:    vs,
				}, "", "  ")
				fmt.Println(string(data))
			} else {
				fmt.Fprintf(os.Stderr, "[quancode] dry-run: would delegate to %s\n", agentKey)
				fmt.Fprintf(os.Stderr, "[quancode] isolation: %s\n", isolation)
				fmt.Fprintf(os.Stderr, "[quancode] work_dir: %s\n", workDir)
				if vs != nil {
					fmt.Fprintf(os.Stderr, "[quancode] verify: %v (strict=%v)\n", vs.Commands, vs.Strict)
				}
				fmt.Fprintf(os.Stderr, "[quancode] --- full prompt ---\n")
				fmt.Print(fullTask)
			}
			return nil
		}

		// Async mode: spawn a detached job-runner and return immediately.
		if delegateAsync {
			if isolation == "inplace" {
				return fmt.Errorf("--async requires --isolation worktree or --isolation patch")
			}
			if vs != nil {
				return fmt.Errorf("--async does not support --verify/--verify-strict (not yet implemented)")
			}
			effectiveTimeout := resolveEffectiveTimeout(delegateTimeout, ac.TimeoutSecs)
			return launchAsyncJob(agentKey, task, workDir, isolation, effectiveTimeout)
		}

		// Resolve fallback: CLI flag > preferences > auto
		noFallback := delegateNoFallback
		if !cmd.Flags().Changed("no-fallback") {
			noFallback = cfg.Preferences.FallbackMode == "off"
		}

		// Speculative parallelism: when enabled and isolation allows it,
		// launch a backup agent in parallel after the delay window.
		specDelay := cfg.Preferences.SpeculativeDelaySecs
		if specDelay > 0 && !noFallback && (isolation == "worktree" || isolation == "patch") {
			err := runSpeculativeDelegation(speculativeDelegationOpts{
				cfg:             cfg,
				primaryAgent:    a,
				primaryKey:      agentKey,
				task:            task,
				workDir:         workDir,
				isolation:       isolation,
				verify:          vs,
				timeoutOverride: delegateTimeout,
				delaySecs:       specDelay,
				noContext:       delegateNoContext,
				contextFiles:    delegateContextFiles,
				contextDiff:     delegateContextDiff,
				contextMaxSize:  delegateContextMaxSize,
			})
			if !errors.Is(err, errNoSpeculativeAgent) {
				return err // speculative ran (nil = success, non-nil = failure)
			}
			// No backup agent available — fall through to normal serial path
		}

		// Run attempt with fallback loop
		fl := newFallbackLoop(cfg, task, agentKey, 0)
		var chain []ui.ChainLink
		runID, err := ledger.NewRunID()
		if err != nil {
			return fmt.Errorf("generate run id: %w", err)
		}
		meta := attemptMeta{RunID: runID, Attempt: 1}

		for {
			// Build context per-agent (agent-specific Context config may differ)
			var ctxPrefix string
			if !delegateNoContext {
				currentAc := cfg.Agents[agentKey]
				builder := qcontext.NewBuilder(cfg.ContextDefaults, currentAc.Context)
				bundle := builder.Build(workDir, delegateContextFiles, delegateContextDiff, delegateContextMaxSize)
				ctxPrefix = qcontext.Format(bundle)
				for _, w := range bundle.Warnings {
					fmt.Fprintf(os.Stderr, "[quancode] context: %s\n", w)
				}
				warnContextSize(bundle, len(ctxPrefix)+len(task))
			}

			ar := runDelegateAttempt(DelegateAttemptOptions{
				Agent:           a,
				AgentKey:        agentKey,
				Task:            task,
				CtxPrefix:       ctxPrefix,
				WorkDir:         workDir,
				Isolation:       isolation,
				Verify:          vs,
				TimeoutOverride: delegateTimeout,
			})

			// Check if fallback is needed and allowed
			shouldFallback := !noFallback && fl.shouldRetry(ar, meta.Attempt)

			// For inplace mode, block fallback if files were changed
			if shouldFallback && (isolation == "" || isolation == "inplace") {
				if !runner.IsGitRepo(workDir) {
					fmt.Fprintf(os.Stderr, "[quancode] %s failed in non-git directory — skipping fallback\n", agentKey)
					shouldFallback = false
				} else if changes := detectNewChanges(workDir, ar.preSnapshot); len(changes) > 0 {
					fmt.Fprintf(os.Stderr, "[quancode] %s failed but modified files — skipping fallback\n", agentKey)
					shouldFallback = false
				}
			}

			if !shouldFallback {
				err := finalizeDelegation(agentKey, task, workDir, isolation, meta, ar)
				if len(chain) > 0 {
					chain = append(chain, ui.ChainLink{Agent: agentKey, FailureClass: ar.failureClass})
					ui.FallbackChain(chain)
				}
				return err
			}

			// Record this failed attempt in the chain
			chain = append(chain, ui.ChainLink{Agent: agentKey, FailureClass: ar.failureClass})
			fmt.Fprintf(os.Stderr, "[quancode] %s %s, looking for fallback...\n", agentKey, ar.failureClass)
			logAttempt(agentKey, task, workDir, isolation, meta, ar)

			previousAgent := agentKey

			nextKey, nextA := fl.nextAgent()
			if nextA == nil {
				fmt.Fprintf(os.Stderr, "[quancode] no fallback agents available\n")
				err := finalizeDelegation(agentKey, task, workDir, isolation, meta, ar)
				chain = append(chain, ui.ChainLink{Agent: agentKey, FailureClass: ar.failureClass})
				ui.FallbackChain(chain)
				return err
			}

			agentKey = nextKey
			a = nextA
			fmt.Fprintf(os.Stderr, "[quancode] falling back to %s\n", agentKey)
			meta.Attempt++
			meta.FallbackFrom = previousAgent
			meta.FallbackReason = ar.failureClass
		}
	},
}

// gitStatusSnapshot captures the current git status as a set of "status filename" lines.
func gitStatusSnapshot(dir string) map[string]bool {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	snapshot := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		if line != "" {
			snapshot[line] = true
		}
	}
	return snapshot
}

// detectNewChanges compares current git status against a pre-delegation snapshot,
// returning only files that are new or changed since the snapshot.
func detectNewChanges(dir string, preSnapshot map[string]bool) []string {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		// Only report lines not present in the pre-delegation snapshot
		if !preSnapshot[line] {
			// Extract filename (after the 3-char status prefix)
			if len(line) > 3 {
				files = append(files, strings.TrimSpace(line[3:]))
			}
		}
	}
	return dedupe(files)
}

func dedupe(items []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

// warnContextSize emits a stderr warning when the total prompt size
// (formatted context + task) is large enough to risk sub-agent timeouts.
func warnContextSize(_ *qcontext.ContextBundle, totalBytes int) {
	const warnThresholdKB = 24 // 24KB — 75% of default 32KB context budget
	totalKB := totalBytes / 1024
	if totalKB >= warnThresholdKB {
		fmt.Fprintf(os.Stderr, "[quancode] warning: total prompt size %dKB is large — consider --no-context, fewer --context-files, or splitting the task\n", totalKB)
	}
}

func init() {
	delegateCmd.Flags().StringVar(&delegateAgent, "agent", "", "target agent (e.g., codex, claude)")
	_ = delegateCmd.RegisterFlagCompletionFunc("agent", completeAgentKeys)
	delegateCmd.Flags().StringVar(&delegateWorkdir, "workdir", "", "working directory (default: current)")
	delegateCmd.Flags().StringVar(&delegateFormat, "format", "text", "output format: text or json")
	_ = delegateCmd.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"text", "json"}, cobra.ShellCompDirectiveNoFileComp
	})
	delegateCmd.Flags().StringVar(&delegateIsolation, "isolation", "", "isolation mode: inplace, worktree, or patch (default from preferences)")
	_ = delegateCmd.RegisterFlagCompletionFunc("isolation", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"inplace", "worktree", "patch"}, cobra.ShellCompDirectiveNoFileComp
	})
	delegateCmd.Flags().BoolVar(&delegateNoFallback, "no-fallback", false, "disable automatic fallback to other agents on failure")
	delegateCmd.Flags().StringArrayVar(&delegateContextFiles, "context-files", nil, "additional context files to include (can be specified multiple times)")
	delegateCmd.Flags().StringVar(&delegateContextDiff, "context-diff", "", "include git diff: staged, working, or empty for off")
	delegateCmd.Flags().IntVar(&delegateContextMaxSize, "context-max-size", 0, "override max context size in bytes (0 = use config default)")
	delegateCmd.Flags().BoolVar(&delegateNoContext, "no-context", false, "disable automatic context injection")
	delegateCmd.Flags().BoolVar(&delegateDryRun, "dry-run", false, "show what would be sent to the agent without executing")
	delegateCmd.Flags().StringArrayVar(&delegateVerify, "verify", nil, "verification command to run after delegation (record only, can be specified multiple times)")
	delegateCmd.Flags().StringArrayVar(&delegateVerifyStrict, "verify-strict", nil, "verification command — fail delegation if verification fails (can be specified multiple times)")
	delegateCmd.Flags().IntVar(&delegateVerifyTimeout, "verify-timeout", 120, "timeout in seconds for each verification command")
	delegateCmd.Flags().BoolVar(&delegateAsync, "async", false, "run delegation asynchronously (requires --isolation worktree or patch)")
	delegateCmd.Flags().IntVar(&delegateTimeout, "timeout", 0, "timeout in seconds (overrides agent config timeout_secs)")
	rootCmd.AddCommand(delegateCmd)
}
