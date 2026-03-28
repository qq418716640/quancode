package router

import (
	"testing"

	"github.com/qq418716640/quancode/config"
)

func TestSelectAgentPrefersKeywordMatch(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude": {Enabled: true},
			"codex": {
				Enabled:      true,
				Priority:     20,
				PreferredFor: []string{"test"},
			},
			"aider": {
				Enabled:  true,
				Priority: 10,
			},
		},
	}

	got := SelectAgent(cfg, "write tests for config loading")
	if got == nil {
		t.Fatal("expected a selection")
	}
	if got.AgentKey != "codex" {
		t.Fatalf("expected codex due to keyword match, got %q", got.AgentKey)
	}
}

func TestSelectAgentFallsBackToPriority(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude": {Enabled: true},
			"codex": {
				Enabled:  true,
				Priority: 20,
			},
			"aider": {
				Enabled:  true,
				Priority: 10,
			},
		},
	}

	got := SelectAgent(cfg, "explain the current architecture")
	if got == nil {
		t.Fatal("expected a selection")
	}
	if got.AgentKey != "aider" {
		t.Fatalf("expected aider due to lower priority number, got %q", got.AgentKey)
	}
}
