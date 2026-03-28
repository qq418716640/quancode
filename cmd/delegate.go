package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/config"
	"github.com/qq418716640/quancode/ledger"
	"github.com/qq418716640/quancode/router"
	"github.com/qq418716640/quancode/runner"
	"github.com/spf13/cobra"
)

var (
	delegateAgent        string
	delegateWorkdir      string
	delegateFormat       string
	delegateIsolation    string
	delegateAutoApprove  bool
	delegateNoFallback   bool
	approvalPollInterval = time.Second
	approvalTimeout      = 120 * time.Second
	// stdinReader is the source for interactive approval prompts.
	// Tests can replace this to avoid blocking on os.Stdin.
	stdinReader *bufio.Reader
)

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
}

func buildDelegationResult(agentKey, task, isolation, output, patch string, result *runner.Result, err error, changedFiles []string, approvalEvents []ledger.ApprovalEvent) DelegationResult {
	dr := DelegationResult{
		Agent:          agentKey,
		Task:           task,
		Isolation:      isolation,
		ApprovalEvents: approvalEvents,
	}
	if result != nil {
		dr.DelegationID = result.DelegationID
		dr.ExitCode = result.ExitCode
		dr.TimedOut = result.TimedOut
		dr.DurationMs = result.DurationMs
		dr.Output = output
		dr.ChangedFiles = changedFiles
		dr.Status = "completed"
		if result.TimedOut {
			dr.Status = "timed_out"
		} else if result.ExitCode != 0 {
			dr.Status = "failed"
		}
	}
	if isolation == "patch" && patch != "" {
		dr.Patch = patch
	}
	if err != nil {
		dr.Output = output
		if dr.ExitCode == 0 {
			dr.ExitCode = 1
		}
		if dr.Status == "" {
			dr.Status = "failed"
		}
	}
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
		if ok, _ := a.IsAvailable(); !ok {
			return fmt.Errorf("agent %s: command %q not found in PATH", agentKey, ac.Command)
		}

		// Run attempt with fallback loop
		tried := map[string]bool{agentKey: true}
		attemptNum := 1

		for {
			ar := runDelegateAttempt(a, agentKey, task, workDir, isolation, cfg)

			// Check if fallback is needed and allowed
			shouldFallback := !delegateNoFallback &&
				attemptNum < 3 &&
				ar.result != nil &&
				isFallbackEligible(ar.result, ar.output)

			// For inplace mode, block fallback if files were changed
			if shouldFallback && (isolation == "" || isolation == "inplace") {
				if changes := detectNewChanges(workDir, ar.preSnapshot); len(changes) > 0 {
					fmt.Fprintf(os.Stderr, "[quancode] %s failed but modified files — skipping fallback\n", agentKey)
					shouldFallback = false
				}
			}

			if !shouldFallback {
				// Final result — format and return
				return finalizeDelegation(agentKey, task, workDir, isolation, ar)
			}

			// Log failure and find fallback
			reason := "timed out"
			if !ar.result.TimedOut {
				reason = "rate-limited or transient error"
			}
			fmt.Fprintf(os.Stderr, "[quancode] %s %s, looking for fallback...\n", agentKey, reason)

			// Log failed attempt to ledger
			logAttempt(agentKey, task, workDir, isolation, ar)

			sel := router.SelectAgentExcluding(cfg, task, tried)
			if sel == nil {
				fmt.Fprintf(os.Stderr, "[quancode] no fallback agents available\n")
				return finalizeDelegation(agentKey, task, workDir, isolation, ar)
			}

			agentKey = sel.AgentKey
			tried[agentKey] = true
			attemptNum++

			ac = cfg.Agents[agentKey]
			a = agent.FromConfig(agentKey, ac)
			if ok, _ := a.IsAvailable(); !ok {
				fmt.Fprintf(os.Stderr, "[quancode] fallback %s not available, skipping\n", agentKey)
				continue
			}

			fmt.Fprintf(os.Stderr, "[quancode] falling back to %s (%s)\n", agentKey, sel.Reason)
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
	rootCmd.AddCommand(delegateCmd)
}
