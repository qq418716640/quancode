package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/approval"
	"github.com/qq418716640/quancode/config"
	qcontext "github.com/qq418716640/quancode/context"
	"github.com/qq418716640/quancode/ledger"
	"github.com/qq418716640/quancode/router"
	"github.com/qq418716640/quancode/runner"
	"github.com/spf13/cobra"
)

var (
	delegateAgent          string
	delegateWorkdir        string
	delegateFormat         string
	delegateIsolation      string
	delegateAutoApprove    bool
	delegateNoFallback     bool
	delegateContextFiles   []string
	delegateContextDiff    string
	delegateContextMaxSize int
	delegateNoContext      bool
	delegateDryRun         bool
	delegateVerify         []string
	delegateVerifyStrict   []string
	delegateVerifyTimeout  int
	approvalPollInterval   = time.Second
	approvalTimeout        = 120 * time.Second
	// stdinReader is the source for interactive approval prompts.
	// Tests can replace this to avoid blocking on os.Stdin.
	stdinReader *bufio.Reader
)

type dryRunResult struct {
	Agent     string      `json:"agent"`
	Task      string      `json:"task"`
	Isolation string      `json:"isolation"`
	WorkDir   string      `json:"work_dir"`
	Verify    *verifySpec `json:"verify,omitempty"`
}

type DelegationResult struct {
	Agent          string                 `json:"agent"`
	Task           string                 `json:"task"`
	DelegationID   string                 `json:"delegation_id,omitempty"`
	Status         string                 `json:"status,omitempty"`
	ExitCode       int                    `json:"exit_code"`
	TimedOut       bool                   `json:"timed_out"`
	DurationMs     int64                  `json:"duration_ms"`
	Output         string                 `json:"output"`
	ChangedFiles   []string               `json:"changed_files"`
	ApprovalEvents []ledger.ApprovalEvent `json:"approval_events,omitempty"`
	Isolation      string                 `json:"isolation,omitempty"`
	Patch          string                 `json:"patch,omitempty"`
	Verify         *VerifyResult          `json:"verify,omitempty"`
	ConflictFiles  []string               `json:"conflict_files,omitempty"`
}

func buildDelegationResult(agentKey, task, isolation string, ar attemptResult) DelegationResult {
	dr := DelegationResult{
		Agent:          agentKey,
		Task:           task,
		Isolation:      isolation,
		ApprovalEvents: ar.approvalEvents,
		Verify:         ar.verify,
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
		isolation := delegateIsolation

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

		// Run attempt with fallback loop
		tried := map[string]bool{agentKey: true}
		runID, err := approval.NewRunID()
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
			}

			ar := runDelegateAttempt(a, agentKey, task, ctxPrefix, workDir, isolation, vs)

			// Check if fallback is needed and allowed
			shouldFallback := !delegateNoFallback &&
				meta.Attempt < 3 &&
				isTransientFailure(ar.failureClass)

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
				return finalizeDelegation(agentKey, task, workDir, isolation, meta, ar)
			}

			fmt.Fprintf(os.Stderr, "[quancode] %s %s, looking for fallback...\n", agentKey, ar.failureClass)
			// FallbackFrom/FallbackReason in meta describe where *this* attempt came from,
			// so the failing attempt's entry has empty values; the next attempt records the link.
			logAttempt(agentKey, task, workDir, isolation, meta, ar)

			previousAgent := agentKey

			// Find next available agent
			found := false
			for {
				sel := router.SelectAgentExcluding(cfg, task, tried)
				if sel == nil {
					break
				}
				tried[sel.AgentKey] = true

				nextAc := cfg.Agents[sel.AgentKey]
				nextA := agent.FromConfig(sel.AgentKey, nextAc)
				if ok, _ := nextA.IsAvailable(); !ok {
					fmt.Fprintf(os.Stderr, "[quancode] fallback %s not available, skipping\n", sel.AgentKey)
					continue
				}

				agentKey = sel.AgentKey
				a = nextA
				found = true
				fmt.Fprintf(os.Stderr, "[quancode] falling back to %s (%s)\n", agentKey, sel.Reason)
				break
			}

			if !found {
				fmt.Fprintf(os.Stderr, "[quancode] no fallback agents available\n")
				return finalizeDelegation(agentKey, task, workDir, isolation, meta, ar)
			}
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

func init() {
	delegateCmd.Flags().StringVar(&delegateAgent, "agent", "", "target agent (e.g., codex, claude)")
	_ = delegateCmd.RegisterFlagCompletionFunc("agent", completeAgentKeys)
	delegateCmd.Flags().StringVar(&delegateWorkdir, "workdir", "", "working directory (default: current)")
	delegateCmd.Flags().StringVar(&delegateFormat, "format", "text", "output format: text or json")
	_ = delegateCmd.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"text", "json"}, cobra.ShellCompDirectiveNoFileComp
	})
	delegateCmd.Flags().StringVar(&delegateIsolation, "isolation", "inplace", "isolation mode: inplace, worktree, or patch")
	_ = delegateCmd.RegisterFlagCompletionFunc("isolation", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"inplace", "worktree", "patch"}, cobra.ShellCompDirectiveNoFileComp
	})
	delegateCmd.Flags().BoolVar(&delegateAutoApprove, "auto-approve", false, "automatically approve all approval requests")
	delegateCmd.Flags().BoolVar(&delegateNoFallback, "no-fallback", false, "disable automatic fallback to other agents on failure")
	delegateCmd.Flags().StringArrayVar(&delegateContextFiles, "context-files", nil, "additional context files to include (can be specified multiple times)")
	delegateCmd.Flags().StringVar(&delegateContextDiff, "context-diff", "", "include git diff: staged, working, or empty for off")
	delegateCmd.Flags().IntVar(&delegateContextMaxSize, "context-max-size", 0, "override max context size in bytes (0 = use config default)")
	delegateCmd.Flags().BoolVar(&delegateNoContext, "no-context", false, "disable automatic context injection")
	delegateCmd.Flags().BoolVar(&delegateDryRun, "dry-run", false, "show what would be sent to the agent without executing")
	delegateCmd.Flags().StringArrayVar(&delegateVerify, "verify", nil, "verification command to run after delegation (record only, can be specified multiple times)")
	delegateCmd.Flags().StringArrayVar(&delegateVerifyStrict, "verify-strict", nil, "verification command — fail delegation if verification fails (can be specified multiple times)")
	delegateCmd.Flags().IntVar(&delegateVerifyTimeout, "verify-timeout", 120, "timeout in seconds for each verification command")
	rootCmd.AddCommand(delegateCmd)
}
