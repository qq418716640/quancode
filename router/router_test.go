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

// fakeHealth implements HealthFilter for tests.
type fakeHealth map[string]string // agent -> reason (presence means open)

func (f fakeHealth) IsOpen(agent string) (bool, string) {
	r, ok := f[agent]
	return ok, r
}

func TestSelectHealthySkipsUnhealthy(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude":  {Enabled: true, Priority: 10},
			"copilot": {Enabled: true, Priority: 20},
			"codex":   {Enabled: true, Priority: 30},
		},
	}
	hf := fakeHealth{"copilot": "66 consecutive failures"}

	sel, probed, skipped := SelectHealthy(cfg, "do a thing", nil, hf, true)
	if sel == nil || sel.AgentKey != "codex" {
		t.Fatalf("got %v, want codex (copilot is unhealthy, claude is primary)", sel)
	}
	if probed {
		t.Error("should not be a forced probe when a healthy agent exists")
	}
	if skipped["copilot"] == "" {
		t.Error("skipped map should explain why copilot was dropped")
	}
}

func TestSelectHealthyForcedProbeWhenAllUnhealthy(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude":  {Enabled: true, Priority: 10},
			"copilot": {Enabled: true, Priority: 20},
		},
	}
	hf := fakeHealth{"copilot": "dead"}

	sel, probed, _ := SelectHealthy(cfg, "task", nil, hf, true)
	if sel == nil || !probed {
		t.Fatalf("expected a forced probe, got sel=%v probed=%v", sel, probed)
	}

	// With probing disallowed, the caller gets nothing rather than a known-dead agent.
	sel2, probed2, _ := SelectHealthy(cfg, "task", nil, hf, false)
	if sel2 != nil || probed2 {
		t.Errorf("allowProbe=false should return nil, got %v", sel2)
	}
}

func TestSelectHealthyNilFilterIsPassthrough(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude": {Enabled: true, Priority: 10},
			"codex":  {Enabled: true, Priority: 20},
		},
	}
	sel, probed, _ := SelectHealthy(cfg, "task", nil, nil, true)
	if sel == nil || sel.AgentKey != "codex" || probed {
		t.Errorf("nil filter should behave like SelectAgentExcluding, got %v", sel)
	}
}

func TestSelectHealthyRespectsExcludeSet(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude": {Enabled: true, Priority: 10},
			"codex":  {Enabled: true, Priority: 20},
			"qoder":  {Enabled: true, Priority: 30},
		},
	}
	sel, _, _ := SelectHealthy(cfg, "task", map[string]bool{"codex": true}, fakeHealth{}, true)
	if sel == nil || sel.AgentKey != "qoder" {
		t.Errorf("got %v, want qoder (codex excluded, claude is primary)", sel)
	}
}
