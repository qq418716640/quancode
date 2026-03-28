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
