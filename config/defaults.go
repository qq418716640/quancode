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
		TimeoutSecs:  480,
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
		TimeoutSecs:  480,
		Enabled:      true,
		PreferredFor: []string{"test", "fix", "generate", "create", "write", "quick"},
		Priority:     20,
	},
	"qoder": {
		Name:             "Qoder CLI",
		Command:          "qodercli",
		Description:      "Strong at code analysis, debugging, MCP integration",
		Strengths:        []string{"code-analysis", "debugging", "explanation", "mcp-integration"},
		DelegateArgs:     []string{"-p"},
		TimeoutSecs:      300,
		Enabled:          true,
		PreferredFor:     []string{"analyze", "explain", "review"},
		Priority:         25,
		DefaultIsolation:    "inplace",                // Qoder ignores worktree cwd; see feedback_qoder_worktree.md
		SupportedIsolations: []string{"inplace"},      // worktree/patch unsupported (upstream cwd issue)
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
		DelegateArgs: []string{"-p"},
		TimeoutSecs:  300,
		Enabled:      true,
		PreferredFor: []string{"explore", "explain", "search"},
		Priority:     30,
	},
	"gemini": {
		Name:         "Gemini CLI",
		Command:      "gemini",
		Description:  "Google Gemini coding agent with tool use and large context",
		Strengths:    []string{"large-context", "multi-modal", "tool-use"},
		DelegateArgs: []string{"-p"},
		TimeoutSecs:  300,
		Enabled:      true,
		PreferredFor: []string{"generate", "explain", "explore"},
		Priority:     30,
	},
	"copilot": {
		Name:         "GitHub Copilot CLI",
		Command:      "copilot",
		Description:  "GitHub Copilot coding agent with repository context",
		Strengths:    []string{"code-generation", "github-integration", "repository-context"},
		DelegateArgs: []string{"-p"},
		TimeoutSecs:  300,
		Enabled:      true,
		PreferredFor: []string{"generate", "github", "suggest"},
		Priority:     30,
	},
	"amp": {
		Name:         "Amp",
		Command:      "amp",
		Description:  "Sourcegraph coding agent with codebase-wide context",
		Strengths:    []string{"codebase-search", "multi-file", "deep-reasoning"},
		DelegateArgs: []string{"-x"},
		TimeoutSecs:  300,
		Enabled:      true,
		PreferredFor: []string{"search", "understand", "navigate"},
		Priority:     35,
	},
	"goose": {
		Name:         "Goose",
		Command:      "goose",
		Description:  "Block's autonomous coding agent with extensible toolkits",
		Strengths:    []string{"autonomous", "extensible", "multi-provider"},
		DelegateArgs: []string{"run", "-t"},
		TimeoutSecs:  300,
		Enabled:      true,
		PreferredFor: []string{"automate", "script", "pipeline"},
		Priority:     35,
	},
	"cline": {
		Name:         "Cline CLI",
		Command:      "cline",
		Description:  "Autonomous coding agent with plan-and-act workflow",
		Strengths:    []string{"autonomous", "plan-act", "tool-use"},
		DelegateArgs: []string{"-y"},
		TimeoutSecs:  300,
		Enabled:      true,
		PreferredFor: []string{"implement", "build", "automate"},
		Priority:     35,
	},
	"kiro": {
		Name:         "Kiro CLI",
		Command:      "kiro-cli",
		Description:  "AWS coding agent with spec-driven development",
		Strengths:    []string{"spec-driven", "aws-integration", "structured-output"},
		DelegateArgs: []string{"chat", "--no-interactive"},
		TimeoutSecs:  300,
		Enabled:      true,
		PreferredFor: []string{"aws", "cloud", "infrastructure"},
		Priority:     35,
	},
	"aichat": {
		Name:         "aichat",
		Command:      "aichat",
		Description:  "Multi-provider chat and agent CLI with RAG support",
		Strengths:    []string{"multi-provider", "rag", "lightweight"},
		DelegateArgs: []string{},
		TimeoutSecs:  300,
		Enabled:      true,
		PreferredFor: []string{"chat", "query", "summarize"},
		Priority:     40,
	},
}

// defaultPreferences is the single source of truth for preference defaults.
var defaultPreferences = Preferences{
	DefaultIsolation: "inplace",
	FallbackMode:     "auto",
}

func DefaultConfig() *Config {
	return &Config{
		DefaultPrimary: "claude",
		Agents: map[string]AgentConfig{
			"claude": KnownAgents["claude"],
			"codex":  KnownAgents["codex"],
		},
		Preferences: defaultPreferences,
	}
}

// applyPreferencesDefaults fills in zero-value preferences with defaults.
func applyPreferencesDefaults(p *Preferences) {
	if p.DefaultIsolation == "" {
		p.DefaultIsolation = defaultPreferences.DefaultIsolation
	}
	if p.FallbackMode == "" {
		p.FallbackMode = defaultPreferences.FallbackMode
	}
}
