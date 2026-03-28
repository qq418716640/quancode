package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/approval"
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

		// Resolve agent
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

		// Resolve working directory
		workDir := delegateWorkdir
		if workDir == "" {
			workDir, _ = os.Getwd()
		}

		// Handle isolation modes
		isolation := delegateIsolation
		execDir := workDir
		var patch string
		var patchFiles []string
		var cleanupWorktree func()

		if isolation == "worktree" || isolation == "patch" {
			if !runner.IsGitRepo(workDir) {
				return fmt.Errorf("--isolation %s requires a git repository", isolation)
			}
			wt, cleanup, wtErr := runner.CreateWorktree(workDir)
			if wtErr != nil {
				return fmt.Errorf("create worktree: %w", wtErr)
			}
			cleanupWorktree = cleanup
			execDir = wt
			fmt.Fprintf(os.Stderr, "[quancode] running in isolated worktree: %s\n", wt)
		}

		// Snapshot git state before delegation (for accurate changed_files)
		preSnapshot := gitStatusSnapshot(execDir)

		fmt.Fprintf(os.Stderr, "[quancode] delegating to %s: %s\n", agentKey, task)
		delegationID, err := approval.NewDelegationID()
		if err != nil {
			return fmt.Errorf("generate delegation id: %w", err)
		}
		approvalDir, err := approval.CreateApprovalDir(delegationID)
		if err != nil {
			return fmt.Errorf("create approval dir: %w", err)
		}
		defer func() {
			if cleanupErr := approval.CleanupApprovalDir(approvalDir); cleanupErr != nil {
				fmt.Fprintf(os.Stderr, "[quancode] warning: failed to clean approval dir: %v\n", cleanupErr)
			}
		}()

		type delegateResult struct {
			result *runner.Result
			err    error
		}
		doneCh := make(chan delegateResult, 1)
		go func() {
			result, err := a.Delegate(execDir, task, agent.DelegateOptions{
				DelegationID: delegationID,
				ApprovalDir:  approvalDir,
			})
			doneCh <- delegateResult{result: result, err: err}
		}()

		var (
			result         *runner.Result
			approvalEvents []ledger.ApprovalEvent
			errResult      error
		)
		pendingSince := make(map[string]time.Time)
		eventIndex := make(map[string]int)
		pollTicker := time.NewTicker(approvalPollInterval)
		defer pollTicker.Stop()

		// promptQueue serialises interactive approval prompts so only one
		// goroutine reads from stdin at a time. Buffer is generous so the
		// poll loop never blocks when queuing requests.
		promptQueue := make(chan *approval.Request, 16)
		loopDone := make(chan struct{}) // closed when the main loop exits

		reader := stdinReader
		if reader == nil {
			reader = bufio.NewReader(os.Stdin)
		}

		// readLine wraps blocking stdin read into a channel so it can be
		// cancelled via loopDone, preventing goroutine leaks and races
		// with the deferred approvalDir cleanup.
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

		// Single goroutine drains promptQueue and reads user input serially.
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

					// Wait for user input or loop exit, whichever comes first.
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
				errResult = done.err
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
						eventIndex[req.RequestID] = len(approvalEvents)
						approvalEvents = append(approvalEvents, ledger.ApprovalEvent{
							RequestID:   req.RequestID,
							Action:      req.Action,
							Description: req.Description,
						})

						if delegateAutoApprove {
							// Write approval directly — no channel, no goroutine, no deadlock risk.
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
							// Queue for the single prompt goroutine.
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
		close(loopDone) // signal prompt goroutine to exit
		err = errResult

		for requestID, idx := range eventIndex {
			resp, readErr := approval.ReadResponse(approvalDir, requestID)
			if readErr == nil {
				approvalEvents[idx].Decision = resp.Decision
			}
		}

		// Collect patch from worktree before cleanup
		if isolation == "worktree" || isolation == "patch" {
			var collectErr error
			patch, patchFiles, collectErr = runner.CollectPatch(execDir)
			if collectErr != nil {
				fmt.Fprintf(os.Stderr, "[quancode] warning: patch collection failed: %v\n", collectErr)
			}

			if isolation == "worktree" && patch != "" {
				// Auto-apply patch to main workdir
				if applyErr := runner.ApplyPatch(workDir, patch); applyErr != nil {
					fmt.Fprintf(os.Stderr, "[quancode] warning: failed to apply patch: %v\n", applyErr)
				} else {
					fmt.Fprintf(os.Stderr, "[quancode] changes applied to working directory\n")
				}
			}

			if cleanupWorktree != nil {
				cleanupWorktree()
			}
		}

		// Build output string from result
		output := ""
		var changedFiles []string
		if result != nil {
			output = result.Stdout
			if output == "" {
				output = result.Stderr
			}
			if len(patchFiles) > 0 {
				changedFiles = patchFiles
			} else if isolation == "" || isolation == "inplace" {
				changedFiles = detectNewChanges(workDir, preSnapshot)
			}
		}

		// Record to ledger
		logEntry := &ledger.Entry{
			Agent:     agentKey,
			Task:      task,
			WorkDir:   workDir,
			Isolation: isolation,
		}
		if result != nil {
			logEntry.ExitCode = result.ExitCode
			logEntry.TimedOut = result.TimedOut
			logEntry.DurationMs = result.DurationMs
			logEntry.ChangedFiles = changedFiles
		}
		logEntry.ApprovalEvents = append(logEntry.ApprovalEvents, approvalEvents...)
		if err != nil && logEntry.ExitCode == 0 {
			logEntry.ExitCode = 1
		}
		if logErr := ledger.Append(logEntry); logErr != nil {
			fmt.Fprintf(os.Stderr, "[quancode] warning: failed to write ledger: %v\n", logErr)
		}

		if delegateFormat == "json" {
			dr := buildDelegationResult(agentKey, task, isolation, output, patch, result, err, changedFiles, approvalEvents)
			data, _ := json.MarshalIndent(dr, "", "  ")
			fmt.Println(string(data))
			if err != nil {
				return &agent.ExitStatusError{Code: 1}
			}
			return nil
		}

		// Text format (default)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[quancode] delegation error: %v\n", err)
			if output != "" {
				fmt.Print(output)
			}
			return &agent.ExitStatusError{Code: 1}
		}
		if isolation == "patch" && patch != "" {
			fmt.Fprintf(os.Stderr, "[quancode] patch (%d files changed, not applied):\n", len(patchFiles))
			fmt.Print(patch)
			return nil
		}
		fmt.Print(output)
		return nil
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
	rootCmd.AddCommand(delegateCmd)
}
