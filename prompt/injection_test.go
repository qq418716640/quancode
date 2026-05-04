package prompt

import (
	"strings"
	"testing"

	"github.com/qq418716640/quancode/config"
)

func TestBuildExcludesPrimaryAndUsesBinaryPath(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude": {
				Name:        "Claude Code",
				Description: "Primary agent",
				Enabled:     true,
			},
			"codex": {
				Name:        "Codex CLI",
				Description: "Secondary agent",
				Strengths:   []string{"tests", "fixes"},
				Enabled:     true,
			},
		},
	}

	out, err := Build(cfg, "claude", "/tmp/quancode")
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if strings.Contains(out, `Claude Code ("claude")`) {
		t.Fatalf("primary agent should be excluded from injected prompt")
	}
	if !strings.Contains(out, `Codex CLI ("codex")`) {
		t.Fatalf("non-primary enabled agent should be included")
	}
	if !strings.Contains(out, `/tmp/quancode delegate --agent <agent-name>`) {
		t.Fatalf("expected injected prompt to use provided binary path")
	}
}

func TestBuildContainsDelegationGuidance(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"codex": {Name: "Codex CLI", Enabled: true},
		},
	}

	out, err := Build(cfg, "claude", "quancode")
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	for _, section := range []string{
		"BEFORE DELEGATING",
		"TIMEOUT CONTROL",
		"TASK TYPES",
		"ISOLATION MODES",
	} {
		if !strings.Contains(out, section) {
			t.Errorf("expected prompt to contain %q section", section)
		}
	}

	// PIPELINE and ASYNC sections are opt-in via Preferences;
	// they MUST NOT appear when the flags are off (default).
	for _, section := range []string{
		"ASYNC DELEGATION",
		"PIPELINE (multi-stage delegation)",
	} {
		if strings.Contains(out, section) {
			t.Errorf("section %q should be omitted by default", section)
		}
	}
}

func TestBuildOptInPipelineAndAsyncSections(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"codex": {Name: "Codex CLI", Enabled: true},
		},
		Preferences: config.Preferences{
			EnablePipelinePrompt: true,
			EnableAsyncPrompt:    true,
		},
	}

	out, err := Build(cfg, "claude", "quancode")
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	for _, section := range []string{
		"ASYNC DELEGATION",
		"PIPELINE (multi-stage delegation)",
	} {
		if !strings.Contains(out, section) {
			t.Errorf("expected prompt to contain %q when opt-in enabled", section)
		}
	}
}

// TestBuildPipelineAndAsyncTogglesAreIndependent verifies the two opt-in
// flags don't leak into each other — enabling one must not include the other.
func TestBuildPipelineAndAsyncTogglesAreIndependent(t *testing.T) {
	cases := []struct {
		name     string
		prefs    config.Preferences
		want     string
		notWant  string
	}{
		{"pipeline-only", config.Preferences{EnablePipelinePrompt: true}, "PIPELINE (multi-stage delegation)", "ASYNC DELEGATION"},
		{"async-only", config.Preferences{EnableAsyncPrompt: true}, "ASYNC DELEGATION", "PIPELINE (multi-stage delegation)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				Agents:      map[string]config.AgentConfig{"codex": {Name: "Codex CLI", Enabled: true}},
				Preferences: tc.prefs,
			}
			out, err := Build(cfg, "claude", "quancode")
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("expected %q present", tc.want)
			}
			if strings.Contains(out, tc.notWant) {
				t.Errorf("expected %q absent", tc.notWant)
			}
		})
	}
}
