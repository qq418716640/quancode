package config

// KnownAgents contains default configs for all known AI coding CLIs.
// Used by `quancode init` to auto-detect and generate config.
var KnownAgents = map[string]AgentConfig{
	"claude": {
		Name:         "Claude Code",
		Command:      "claude",
		Description:  "Strong at architecture, complex reasoning, multi-file edits",
		Strengths:    []string{"architecture", "complex-reasoning", "debugging", "multi-file-edits"},
		PrimaryArgs:  []string{"--append-system-prompt"},
		DelegateArgs: []string{"-p", "--output-format", "text"},
		TimeoutSecs:  300,
		Enabled:      true,
		PreferredFor: []string{"architecture", "refactor", "debug", "design", "plan"},
		Priority:     10,
	},
	"codex": {
		Name:         "Codex CLI",
		Command:      "codex",
		Description:  "Strong at quick edits, code generation, test writing",
		Strengths:    []string{"quick-edits", "code-generation", "test-writing"},
		PrimaryArgs:  []string{},
		PromptMode:   "file",
		PromptFile:   "AGENTS.md",
		DelegateArgs: []string{"exec", "--full-auto", "--ephemeral"},
		OutputFlag:   "--output-last-message",
		TimeoutSecs:  180,
		Enabled:      true,
		PreferredFor: []string{"test", "fix", "generate", "create", "write", "quick"},
		Priority:     20,
	},
	"aider": {
		Name:         "Aider",
		Command:      "aider",
		Description:  "Strong at pair programming, incremental edits, git-aware changes",
		Strengths:    []string{"pair-programming", "incremental-edits", "git-integration"},
		DelegateArgs: []string{"--message"},
		TimeoutSecs:  300,
		Enabled:      true,
		PreferredFor: []string{"edit", "pair", "incremental"},
		Priority:     30,
	},
	"opencode": {
		Name:         "OpenCode",
		Command:      "opencode",
		Description:  "Strong at exploration, code explanation, multi-model support",
		Strengths:    []string{"exploration", "explanation", "multi-model"},
		DelegateArgs: []string{},
		TimeoutSecs:  300,
		Enabled:      true,
		PreferredFor: []string{"explore", "explain", "search"},
		Priority:     30,
	},
}

func DefaultConfig() *Config {
	return &Config{
		DefaultPrimary: "claude",
		Agents: map[string]AgentConfig{
			"claude": KnownAgents["claude"],
			"codex":  KnownAgents["codex"],
		},
	}
}
