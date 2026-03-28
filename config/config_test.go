package config

import "testing"

func TestApplyKnownAgentDefaultsBackfillsPromptFields(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "codex",
		Agents: map[string]AgentConfig{
			"codex": {
				Name:    "Codex CLI",
				Command: "codex",
				Enabled: true,
			},
		},
	}

	applyKnownAgentDefaults(cfg)

	ac := cfg.Agents["codex"]
	if ac.PromptMode != "file" {
		t.Fatalf("expected prompt_mode=file, got %q", ac.PromptMode)
	}
	if ac.PromptFile != "AGENTS.md" {
		t.Fatalf("expected prompt_file=AGENTS.md, got %q", ac.PromptFile)
	}
}

func TestApplyKnownAgentDefaultsKeepsExplicitValues(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "codex",
		Agents: map[string]AgentConfig{
			"codex": {
				Name:       "Codex CLI",
				Command:    "codex",
				Enabled:    true,
				PromptMode: "env",
				PromptFile: "CUSTOM.md",
			},
		},
	}

	applyKnownAgentDefaults(cfg)

	ac := cfg.Agents["codex"]
	if ac.PromptMode != "env" {
		t.Fatalf("expected explicit prompt_mode to be preserved, got %q", ac.PromptMode)
	}
	if ac.PromptFile != "CUSTOM.md" {
		t.Fatalf("expected explicit prompt_file to be preserved, got %q", ac.PromptFile)
	}
}
