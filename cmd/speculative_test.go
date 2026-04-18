package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/config"
	"github.com/qq418716640/quancode/runner"
)

// fakeAgent implements agent.Agent for testing speculative execution.
// sleepBefore controls how long DelegateWithContext blocks before returning
// the prepared result; when the context is cancelled first, the agent returns
// a Cancelled result instead (mirroring runner behavior).
type fakeAgent struct {
	name        string
	result      *runner.Result
	err         error
	available   bool
	sleepBefore time.Duration
}

func (f *fakeAgent) Name() string { return f.name }
func (f *fakeAgent) LaunchAsPrimary(workDir, systemPrompt string) error {
	return nil
}
func (f *fakeAgent) Delegate(workDir, task string, opts agent.DelegateOptions) (*runner.Result, error) {
	return f.result, f.err
}
func (f *fakeAgent) DelegateWithContext(ctx context.Context, workDir, task string, opts agent.DelegateOptions) (*runner.Result, error) {
	if f.sleepBefore <= 0 {
		return f.result, f.err
	}
	select {
	case <-time.After(f.sleepBefore):
		return f.result, f.err
	case <-ctx.Done():
		r := &runner.Result{ExitCode: 124, Cancelled: true}
		if f.result != nil {
			r.Stdout = f.result.Stdout
		}
		return r, fmt.Errorf("cancelled")
	}
}
func (f *fakeAgent) IsAvailable() (bool, string) {
	return f.available, ""
}

func TestSpeculativeNoBackupAgent(t *testing.T) {
	isolateHome(t)

	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude": {
				Name:    "Claude Code",
				Command: "echo",
				Enabled: true,
			},
			// Only one non-primary agent, and we'll use it as primary
		},
	}

	err := runSpeculativeDelegation(speculativeDelegationOpts{
		cfg:          cfg,
		primaryAgent: &fakeAgent{name: "Claude", available: true},
		primaryKey:   "claude",
		task:         "test task",
		workDir:      t.TempDir(),
		isolation:    "worktree",
		delaySecs:    30,
		noContext:    true,
	})

	if err != errNoSpeculativeAgent {
		t.Fatalf("expected errNoSpeculativeAgent, got %v", err)
	}
}

// TestClassifyFailureSpeculativeCancelled verifies the constant exists for backward compat.
func TestClassifyFailureSpeculativeCancelled(t *testing.T) {
	if FailureClassSpeculativeCancelled != "speculative_cancelled" {
		t.Fatalf("unexpected failure class: %s", FailureClassSpeculativeCancelled)
	}
}

// TestDetermineFinalStatusCancelled verifies the new cancelled final status.
func TestDetermineFinalStatusCancelled(t *testing.T) {
	// cancelled takes precedence over timed_out and exit code
	if got := determineFinalStatus(124, true, true, nil); got != StatusCancelled {
		t.Fatalf("cancelled+timedOut: want %s, got %s", StatusCancelled, got)
	}
	if got := determineFinalStatus(124, false, true, nil); got != StatusCancelled {
		t.Fatalf("cancelled only: want %s, got %s", StatusCancelled, got)
	}
	if got := determineFinalStatus(124, true, false, nil); got != StatusTimedOut {
		t.Fatalf("timed_out only: want %s, got %s", StatusTimedOut, got)
	}
	if got := determineFinalStatus(0, false, false, nil); got != StatusCompleted {
		t.Fatalf("success: want %s, got %s", StatusCompleted, got)
	}
}

// TestClassifyFailureCancelledResult verifies cancelled Result classifies correctly.
func TestClassifyFailureCancelledResult(t *testing.T) {
	ar := attemptResult{result: &runner.Result{ExitCode: 124, Cancelled: true}}
	if got := classifyFailure(ar); got != FailureClassSpeculativeCancelled {
		t.Fatalf("cancelled: want %s, got %s", FailureClassSpeculativeCancelled, got)
	}
	// Cancelled + TimedOut both true: cancelled wins (they should be mutually
	// exclusive in practice but defensive test documents the precedence).
	ar.result.TimedOut = true
	if got := classifyFailure(ar); got != FailureClassSpeculativeCancelled {
		t.Fatalf("cancelled+timedOut: want %s, got %s", FailureClassSpeculativeCancelled, got)
	}
	// TimedOut alone still timed_out.
	ar.result.Cancelled = false
	if got := classifyFailure(ar); got != FailureClassTimedOut {
		t.Fatalf("timedOut alone: want %s, got %s", FailureClassTimedOut, got)
	}
}

// TestSpeculativeCancelsSpecWhenPrimaryWins verifies that when the primary
// agent succeeds during the parallel window, the speculative backup is
// cancelled rather than being allowed to run to its full timeout.
//
// This is an integration test that exercises the full speculative delegation
// path with real goroutines and timing. It uses short delays (1s window,
// ~1.3s primary) to keep wall-clock cost low.
func TestSpeculativeCancelsSpecWhenPrimaryWins(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing-sensitive speculative test in short mode")
	}
	isolateHome(t)

	primaryResult := &runner.Result{ExitCode: 0, Stdout: "primary output"}
	specResult := &runner.Result{ExitCode: 0, Stdout: "spec output (should not be seen)"}

	cfg := &config.Config{
		DefaultPrimary: "primary-agent",
		Agents: map[string]config.AgentConfig{
			"primary-agent": {Name: "Primary", Command: "echo", Enabled: true, TimeoutSecs: 10},
			"spec-agent":    {Name: "Spec", Command: "echo", Enabled: true, TimeoutSecs: 10, Priority: 50},
		},
	}

	// Register spec-agent as a FromConfig-able agent by also adding it to
	// the router's candidate pool. SelectAgentExcluding reads from cfg.Agents
	// so that's sufficient. But agent.FromConfig will be called by
	// runSpeculativeDelegation to materialize it — we need to intercept that.
	//
	// Workaround: inject fake agents via the fakeAgentFactory test hook.
	oldFactory := agentFromConfig
	agentFromConfig = func(key string, ac config.AgentConfig) agent.Agent {
		if key == "spec-agent" {
			return &fakeAgent{name: "Spec", available: true, sleepBefore: 3 * time.Second, result: specResult}
		}
		return oldFactory(key, ac)
	}
	defer func() { agentFromConfig = oldFactory }()

	oldFormat := delegateFormat
	delegateFormat = "text"
	defer func() { delegateFormat = oldFormat }()

	workDir := t.TempDir()
	out := captureStdout(t, func() {
		err := runSpeculativeDelegation(speculativeDelegationOpts{
			cfg:          cfg,
			primaryAgent: &fakeAgent{name: "Primary", available: true, sleepBefore: 1300 * time.Millisecond, result: primaryResult},
			primaryKey:   "primary-agent",
			task:         "test",
			workDir:      workDir,
			isolation:    "inplace",
			delaySecs:    1,
			noContext:    true,
		})
		if err != nil {
			t.Fatalf("runSpeculativeDelegation: %v", err)
		}
	})

	if !strings.Contains(out, "primary output") {
		t.Fatalf("expected primary output in stdout, got %q", out)
	}

	// Verify ledger recorded spec-agent as cancelled.
	home := os.Getenv("HOME")
	logs, err := filepath.Glob(filepath.Join(home, ".config", "quancode", "logs", "*.jsonl"))
	if err != nil || len(logs) == 0 {
		t.Fatalf("no ledger logs found: %v", err)
	}
	data, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var foundPrimary, foundSpec bool
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("parse ledger line: %v", err)
		}
		switch entry["agent"] {
		case "primary-agent":
			foundPrimary = true
			if entry["final_status"] != StatusCompleted {
				t.Errorf("primary final_status: want %s, got %v", StatusCompleted, entry["final_status"])
			}
		case "spec-agent":
			foundSpec = true
			if entry["final_status"] != StatusCancelled {
				t.Errorf("spec final_status: want %s, got %v (failure_class=%v, cancelled=%v, timed_out=%v)",
					StatusCancelled, entry["final_status"], entry["failure_class"], entry["cancelled"], entry["timed_out"])
			}
			if entry["failure_class"] != FailureClassSpeculativeCancelled {
				t.Errorf("spec failure_class: want %s, got %v", FailureClassSpeculativeCancelled, entry["failure_class"])
			}
			if entry["cancelled"] != true {
				t.Errorf("spec entry.cancelled: want true, got %v", entry["cancelled"])
			}
			if entry["timed_out"] == true {
				t.Errorf("spec entry.timed_out: should be false for cancelled, got true")
			}
		}
	}
	if !foundPrimary || !foundSpec {
		t.Fatalf("missing ledger entries: primary=%v spec=%v", foundPrimary, foundSpec)
	}
}

// TestSpeculativeWaitsBothWhenSpecWinsFirst verifies that when the spec agent
// finishes first, the primary is NOT cancelled — preserving primary_preferred
// semantics (the primary may succeed milliseconds later and should be
// selected even then).
func TestSpeculativeWaitsBothWhenSpecWinsFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing-sensitive speculative test in short mode")
	}
	isolateHome(t)

	primaryResult := &runner.Result{ExitCode: 0, Stdout: "primary output"}
	specResult := &runner.Result{ExitCode: 0, Stdout: "spec output"}

	cfg := &config.Config{
		DefaultPrimary: "primary-agent",
		Agents: map[string]config.AgentConfig{
			"primary-agent": {Name: "Primary", Command: "echo", Enabled: true, TimeoutSecs: 10},
			"spec-agent":    {Name: "Spec", Command: "echo", Enabled: true, TimeoutSecs: 10, Priority: 50},
		},
	}

	// Spec is launched at T=delaySecs=1s, so its wall-clock arrival is
	// 1s+sleepBefore. To make spec arrive first we need:
	//   primarySleep > 1s + specSleep
	// We use primary=2800ms (arrives T=2.8s), spec=800ms (arrives T=1.8s).
	oldFactory := agentFromConfig
	agentFromConfig = func(key string, ac config.AgentConfig) agent.Agent {
		if key == "spec-agent" {
			return &fakeAgent{name: "Spec", available: true, sleepBefore: 800 * time.Millisecond, result: specResult}
		}
		return oldFactory(key, ac)
	}
	defer func() { agentFromConfig = oldFactory }()

	oldFormat := delegateFormat
	delegateFormat = "text"
	defer func() { delegateFormat = oldFormat }()

	workDir := t.TempDir()
	captureStdout(t, func() {
		err := runSpeculativeDelegation(speculativeDelegationOpts{
			cfg:          cfg,
			primaryAgent: &fakeAgent{name: "Primary", available: true, sleepBefore: 2800 * time.Millisecond, result: primaryResult},
			primaryKey:   "primary-agent",
			task:         "test",
			workDir:      workDir,
			isolation:    "inplace",
			delaySecs:    1,
			noContext:    true,
		})
		if err != nil {
			t.Fatalf("runSpeculativeDelegation: %v", err)
		}
	})

	// Verify both completed, selection went to primary.
	home := os.Getenv("HOME")
	logs, _ := filepath.Glob(filepath.Join(home, ".config", "quancode", "logs", "*.jsonl"))
	if len(logs) == 0 {
		t.Fatalf("no ledger logs")
	}
	data, _ := os.ReadFile(logs[0])
	var primaryStatus, specStatus string
	var primarySelected bool
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		json.Unmarshal([]byte(line), &entry)
		if entry["agent"] == "primary-agent" {
			primaryStatus, _ = entry["final_status"].(string)
			primarySelected, _ = entry["selected"].(bool)
		}
		if entry["agent"] == "spec-agent" {
			specStatus, _ = entry["final_status"].(string)
		}
	}
	if primaryStatus != StatusCompleted {
		t.Errorf("primary should be completed, got %s", primaryStatus)
	}
	if specStatus != StatusCompleted {
		t.Errorf("spec should be completed (not cancelled) when it finished first, got %s", specStatus)
	}
	if !primarySelected {
		t.Errorf("primary should be selected (primary_preferred semantics), selected=false")
	}
}

func TestSpeculativeInfoJSON(t *testing.T) {
	info := &SpeculativeInfo{
		Mode:            "collected",
		Selected:        "primary",
		SelectionReason: "primary_preferred",
		Companion: &CompanionResult{
			Agent:      "copilot",
			Status:     "completed",
			Output:     "test output",
			DurationMs: 5000,
		},
	}
	if info.Mode != "collected" {
		t.Fatalf("unexpected mode: %s", info.Mode)
	}
	if info.Companion.Agent != "copilot" {
		t.Fatalf("unexpected companion agent: %s", info.Companion.Agent)
	}
}
