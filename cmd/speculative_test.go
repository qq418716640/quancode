package cmd

import (
	"context"
	"testing"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/config"
	"github.com/qq418716640/quancode/runner"
)

// fakeAgent implements agent.Agent for testing speculative execution.
type fakeAgent struct {
	name      string
	result    *runner.Result
	err       error
	available bool
}

func (f *fakeAgent) Name() string { return f.name }
func (f *fakeAgent) LaunchAsPrimary(workDir, systemPrompt string) error {
	return nil
}
func (f *fakeAgent) Delegate(workDir, task string, opts agent.DelegateOptions) (*runner.Result, error) {
	return f.result, f.err
}
func (f *fakeAgent) DelegateWithContext(ctx context.Context, workDir, task string, opts agent.DelegateOptions) (*runner.Result, error) {
	return f.result, f.err
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

func TestLoserOf(t *testing.T) {
	primary := &speculativeResult{agentKey: "claude", role: "primary"}
	spec := &speculativeResult{agentKey: "codex", role: "speculative"}

	loser := loserOf(primary, spec, "primary")
	if loser.agentKey != "codex" {
		t.Fatalf("expected codex as loser when primary wins, got %s", loser.agentKey)
	}

	loser = loserOf(primary, spec, "speculative")
	if loser.agentKey != "claude" {
		t.Fatalf("expected claude as loser when speculative wins, got %s", loser.agentKey)
	}
}

func TestClassifyFailureSpeculativeCancelled(t *testing.T) {
	// Verify the new failure class constant exists and is a string
	if FailureClassSpeculativeCancelled != "speculative_cancelled" {
		t.Fatalf("unexpected failure class: %s", FailureClassSpeculativeCancelled)
	}
}
