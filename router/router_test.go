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

func TestSelectAgentExcludingSkipsExcluded(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude": {Enabled: true},
			"codex":  {Enabled: true, Priority: 20},
			"qoder":  {Enabled: true, Priority: 25},
		},
	}

	exclude := map[string]bool{"codex": true}
	got := SelectAgentExcluding(cfg, "some task", exclude)
	if got == nil {
		t.Fatal("expected a selection")
	}
	if got.AgentKey != "qoder" {
		t.Fatalf("expected qoder (codex excluded), got %q", got.AgentKey)
	}
}

func TestSelectAgentExcludingAllExcluded(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude": {Enabled: true},
			"codex":  {Enabled: true, Priority: 20},
		},
	}

	exclude := map[string]bool{"codex": true}
	got := SelectAgentExcluding(cfg, "some task", exclude)
	if got != nil {
		t.Fatalf("expected nil when all non-primary agents excluded, got %v", got)
	}
}

func TestSelectAgentExcludingKeywordMatch(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude": {Enabled: true},
			"codex":  {Enabled: true, Priority: 20, PreferredFor: []string{"test"}},
			"qoder":  {Enabled: true, Priority: 25, PreferredFor: []string{"analyze"}},
			"aider":  {Enabled: true, Priority: 10},
		},
	}

	// Exclude codex, qoder should match "analyze" keyword
	exclude := map[string]bool{"codex": true}
	got := SelectAgentExcluding(cfg, "analyze this code", exclude)
	if got == nil {
		t.Fatal("expected a selection")
	}
	if got.AgentKey != "qoder" {
		t.Fatalf("expected qoder (keyword match), got %q", got.AgentKey)
	}
}

func TestSelectAgentExcludingFallbackReason(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude": {Enabled: true},
			"codex":  {Enabled: true, Priority: 20},
		},
	}

	got := SelectAgentExcluding(cfg, "some task", nil)
	if got == nil {
		t.Fatal("expected a selection")
	}
	if got.AgentKey != "codex" {
		t.Fatalf("expected codex, got %q", got.AgentKey)
	}
	// Reason should indicate fallback
	if got.Reason == "" {
		t.Fatal("expected non-empty reason")
	}
}

func TestSelectAgentNoAgents(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude": {Enabled: true},
		},
	}

	got := SelectAgent(cfg, "some task")
	if got != nil {
		t.Fatalf("expected nil when only primary agent exists, got %v", got)
	}
}

func TestSelectAgentDisabledSkipped(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude": {Enabled: true},
			"codex":  {Enabled: false, Priority: 10},
			"qoder":  {Enabled: true, Priority: 20},
		},
	}

	got := SelectAgent(cfg, "some task")
	if got == nil {
		t.Fatal("expected a selection")
	}
	if got.AgentKey != "qoder" {
		t.Fatalf("expected qoder (codex disabled), got %q", got.AgentKey)
	}
}
