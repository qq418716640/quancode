package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestApplyKnownAgentDefaultsBackfillsDiagnosticHints(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "copilot",
		Agents: map[string]AgentConfig{
			"copilot": {Name: "Copilot", Command: "copilot", Enabled: true},
		},
	}
	applyKnownAgentDefaults(cfg)

	hints := cfg.Agents["copilot"].DiagnosticHints
	if len(hints) == 0 {
		t.Fatalf("expected copilot DiagnosticHints to be backfilled, got empty")
	}
	found := false
	for _, h := range hints {
		if strings.Contains(h.Pattern, "Access denied") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'Access denied' pattern in backfilled hints, got %+v", hints)
	}
}

func TestApplyKnownAgentDefaultsKeepsExplicitDiagnosticHints(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "copilot",
		Agents: map[string]AgentConfig{
			"copilot": {
				Name:            "Copilot",
				Command:         "copilot",
				Enabled:         true,
				DiagnosticHints: []DiagnosticHint{{Pattern: "custom", Hint: "user hint"}},
			},
		},
	}
	applyKnownAgentDefaults(cfg)

	hints := cfg.Agents["copilot"].DiagnosticHints
	if len(hints) != 1 || hints[0].Pattern != "custom" {
		t.Fatalf("expected user DiagnosticHints preserved, got %+v", hints)
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

func TestLoadExplicitPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "quancode.yaml")
	content := []byte(`default_primary: claude
agents:
  claude:
    name: Claude Code
    command: claude
    enabled: true
    timeout_secs: 120
`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultPrimary != "claude" {
		t.Fatalf("expected default_primary=claude, got %q", cfg.DefaultPrimary)
	}
	ac, ok := cfg.Agents["claude"]
	if !ok {
		t.Fatal("expected claude agent in config")
	}
	if ac.Command != "claude" {
		t.Fatalf("expected command=claude, got %q", ac.Command)
	}
	if ac.TimeoutSecs != 120 {
		t.Fatalf("expected timeout_secs=120, got %d", ac.TimeoutSecs)
	}
	if !ac.Enabled {
		t.Fatal("expected enabled=true")
	}
}

func TestLoadExplicitPathNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/quancode.yaml")
	if err == nil {
		t.Fatal("expected error for missing explicit config path")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(cfgPath, []byte("{{not valid yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadFallsBackToDefaults(t *testing.T) {
	// Use a temp dir as HOME so no user config is found, and run from a dir
	// with no quancode.yaml.
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	origWd, _ := os.Getwd()
	defer func() {
		os.Setenv("HOME", origHome)
		os.Chdir(origWd)
	}()
	os.Setenv("HOME", dir)
	os.Chdir(dir)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultPrimary != "claude" {
		t.Fatalf("expected default_primary=claude from defaults, got %q", cfg.DefaultPrimary)
	}
	if _, ok := cfg.Agents["claude"]; !ok {
		t.Fatal("expected claude agent from defaults")
	}
	if _, ok := cfg.Agents["codex"]; !ok {
		t.Fatal("expected codex agent from defaults")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.DefaultPrimary != "claude" {
		t.Fatalf("expected default_primary=claude, got %q", cfg.DefaultPrimary)
	}
	if len(cfg.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(cfg.Agents))
	}

	claude, ok := cfg.Agents["claude"]
	if !ok {
		t.Fatal("expected claude agent")
	}
	if claude.Command != "claude" {
		t.Fatalf("expected claude command=claude, got %q", claude.Command)
	}
	if !claude.Enabled {
		t.Fatal("expected claude enabled=true")
	}

	codex, ok := cfg.Agents["codex"]
	if !ok {
		t.Fatal("expected codex agent")
	}
	if codex.Command != "codex" {
		t.Fatalf("expected codex command=codex, got %q", codex.Command)
	}
	if !codex.Enabled {
		t.Fatal("expected codex enabled=true")
	}
	if codex.PromptMode != "file" {
		t.Fatalf("expected codex prompt_mode=file, got %q", codex.PromptMode)
	}
}

func TestValidateValidConfig(t *testing.T) {
	cfg := DefaultConfig()
	problems := cfg.Validate()
	if len(problems) != 0 {
		t.Fatalf("expected no problems, got %v", problems)
	}
}

func TestValidateEmptyAgents(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "claude",
		Agents:         map[string]AgentConfig{},
	}
	problems := cfg.Validate()
	if len(problems) == 0 {
		t.Fatal("expected problems for empty agents")
	}
	found := false
	for _, p := range problems {
		if p == "no agents configured" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'no agents configured' problem, got %v", problems)
	}
}

func TestValidateMissingDefaultPrimary(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "",
		Agents: map[string]AgentConfig{
			"claude": {Command: "claude", Enabled: true},
		},
	}
	problems := cfg.Validate()
	found := false
	for _, p := range problems {
		if p == "default_primary is not set" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'default_primary is not set' problem, got %v", problems)
	}
}

func TestValidateDefaultPrimaryNotInAgents(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "nonexistent",
		Agents: map[string]AgentConfig{
			"claude": {Command: "claude", Enabled: true},
		},
	}
	problems := cfg.Validate()
	found := false
	for _, p := range problems {
		if p == `default_primary "nonexistent" not found in agents` {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected default_primary not found problem, got %v", problems)
	}
}

func TestValidateDefaultPrimaryDisabled(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "claude",
		Agents: map[string]AgentConfig{
			"claude": {Command: "claude", Enabled: false},
		},
	}
	problems := cfg.Validate()
	found := false
	for _, p := range problems {
		if p == `default_primary "claude" is disabled` {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected default_primary disabled problem, got %v", problems)
	}
}

func TestValidateEmptyCommand(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "claude",
		Agents: map[string]AgentConfig{
			"claude": {Command: "", Enabled: true},
		},
	}
	problems := cfg.Validate()
	found := false
	for _, p := range problems {
		if p == `agent "claude": command is empty` {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected empty command problem, got %v", problems)
	}
}

func TestValidateNegativeTimeout(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "claude",
		Agents: map[string]AgentConfig{
			"claude": {Command: "claude", Enabled: true, TimeoutSecs: -1},
		},
	}
	problems := cfg.Validate()
	found := false
	for _, p := range problems {
		if p == `agent "claude": timeout_secs is negative` {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected negative timeout problem, got %v", problems)
	}
}

func TestApplyKnownAgentDefaultsUnknownAgent(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "myagent",
		Agents: map[string]AgentConfig{
			"myagent": {
				Name:       "My Agent",
				Command:    "myagent",
				Enabled:    true,
				PromptMode: "",
				TaskMode:   "",
				OutputMode: "",
			},
		},
	}

	applyKnownAgentDefaults(cfg)

	ac := cfg.Agents["myagent"]
	if ac.PromptMode != "" {
		t.Fatalf("expected empty prompt_mode for unknown agent, got %q", ac.PromptMode)
	}
	if ac.TaskMode != "" {
		t.Fatalf("expected empty task_mode for unknown agent, got %q", ac.TaskMode)
	}
	if ac.OutputMode != "" {
		t.Fatalf("expected empty output_mode for unknown agent, got %q", ac.OutputMode)
	}
}

// --- Enum validation tests ---

func TestValidateInvalidPromptMode(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "a",
		Agents: map[string]AgentConfig{
			"a": {Command: "a", Enabled: true, PromptMode: "bad"},
		},
	}
	problems := cfg.Validate()
	found := false
	for _, p := range problems {
		if strings.Contains(p, "invalid prompt_mode") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected invalid prompt_mode problem, got %v", problems)
	}
}

func TestValidateInvalidTaskMode(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "a",
		Agents: map[string]AgentConfig{
			"a": {Command: "a", Enabled: true, TaskMode: "bad"},
		},
	}
	problems := cfg.Validate()
	found := false
	for _, p := range problems {
		if strings.Contains(p, "invalid task_mode") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected invalid task_mode problem, got %v", problems)
	}
}

func TestValidateInvalidOutputMode(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "a",
		Agents: map[string]AgentConfig{
			"a": {Command: "a", Enabled: true, OutputMode: "bad"},
		},
	}
	problems := cfg.Validate()
	found := false
	for _, p := range problems {
		if strings.Contains(p, "invalid output_mode") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected invalid output_mode problem, got %v", problems)
	}
}

func TestValidateValidEnumValues(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "a",
		Agents: map[string]AgentConfig{
			"a": {Command: "a", Enabled: true, PromptMode: "file", TaskMode: "stdin", OutputMode: "file"},
		},
	}
	problems := cfg.Validate()
	for _, p := range problems {
		if strings.Contains(p, "invalid") {
			t.Fatalf("unexpected validation problem: %s", p)
		}
	}
}

func TestValidateEmptyEnumValuesAreValid(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "a",
		Agents: map[string]AgentConfig{
			"a": {Command: "a", Enabled: true},
		},
	}
	problems := cfg.Validate()
	for _, p := range problems {
		if strings.Contains(p, "invalid") {
			t.Fatalf("empty enum values should be valid, got: %s", p)
		}
	}
}

// --- Preferences tests ---

func TestPreferencesDefaults(t *testing.T) {
	p := Preferences{}
	applyPreferencesDefaults(&p)
	if p.DefaultIsolation != "inplace" {
		t.Fatalf("expected default_isolation=inplace, got %q", p.DefaultIsolation)
	}
	if p.FallbackMode != "auto" {
		t.Fatalf("expected fallback_mode=auto, got %q", p.FallbackMode)
	}
}

func TestPreferencesPreservesExplicitValues(t *testing.T) {
	p := Preferences{DefaultIsolation: "worktree", FallbackMode: "off"}
	applyPreferencesDefaults(&p)
	if p.DefaultIsolation != "worktree" {
		t.Fatalf("expected worktree preserved, got %q", p.DefaultIsolation)
	}
	if p.FallbackMode != "off" {
		t.Fatalf("expected off preserved, got %q", p.FallbackMode)
	}
}

func TestLoadAppliesPreferencesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "quancode.yaml")
	content := []byte("default_primary: claude\nagents:\n  claude:\n    command: claude\n    enabled: true\n")
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Preferences.DefaultIsolation != "inplace" {
		t.Fatalf("expected preferences backfilled to inplace, got %q", cfg.Preferences.DefaultIsolation)
	}
	if cfg.Preferences.FallbackMode != "auto" {
		t.Fatalf("expected preferences backfilled to auto, got %q", cfg.Preferences.FallbackMode)
	}
}

func TestLoadPreservesExplicitPreferences(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "quancode.yaml")
	content := []byte("default_primary: claude\nagents:\n  claude:\n    command: claude\n    enabled: true\npreferences:\n  default_isolation: patch\n  fallback_mode: \"off\"\n")
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Preferences.DefaultIsolation != "patch" {
		t.Fatalf("expected patch, got %q", cfg.Preferences.DefaultIsolation)
	}
	if cfg.Preferences.FallbackMode != "off" {
		t.Fatalf("expected off, got %q", cfg.Preferences.FallbackMode)
	}
}

func TestValidateInvalidPreferences(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Preferences.DefaultIsolation = "bad"
	cfg.Preferences.FallbackMode = "bad"
	cfg.Preferences.DashboardMode = "bad"
	problems := cfg.Validate()
	foundIso, foundFb, foundDash := false, false, false
	for _, p := range problems {
		if strings.Contains(p, "invalid default_isolation") {
			foundIso = true
		}
		if strings.Contains(p, "invalid fallback_mode") {
			foundFb = true
		}
		if strings.Contains(p, "invalid dashboard_mode") {
			foundDash = true
		}
	}
	if !foundIso {
		t.Fatalf("expected invalid default_isolation problem, got %v", problems)
	}
	if !foundFb {
		t.Fatalf("expected invalid fallback_mode problem, got %v", problems)
	}
	if !foundDash {
		t.Fatalf("expected invalid dashboard_mode problem, got %v", problems)
	}
}

func TestEffectiveDashboardPort(t *testing.T) {
	p := &Preferences{}
	if got := p.EffectiveDashboardPort(); got != DefaultDashboardPort {
		t.Fatalf("expected %d, got %d", DefaultDashboardPort, got)
	}
	p.DashboardPort = 9999
	if got := p.EffectiveDashboardPort(); got != 9999 {
		t.Fatalf("expected 9999, got %d", got)
	}
}

func TestDefaultConfigHasPreferences(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Preferences.DefaultIsolation != "inplace" {
		t.Fatalf("expected inplace, got %q", cfg.Preferences.DefaultIsolation)
	}
	if cfg.Preferences.FallbackMode != "auto" {
		t.Fatalf("expected auto, got %q", cfg.Preferences.FallbackMode)
	}
}

func TestSupportsIsolation(t *testing.T) {
	tests := []struct {
		name      string
		supported []string
		mode      string
		want      bool
	}{
		{"empty list supports all", nil, "worktree", true},
		{"empty list supports inplace", nil, "inplace", true},
		{"explicit match", []string{"inplace"}, "inplace", true},
		{"explicit mismatch", []string{"inplace"}, "worktree", false},
		{"multiple supported", []string{"inplace", "worktree"}, "worktree", true},
		{"multiple not matched", []string{"inplace", "worktree"}, "patch", false},
		{"empty string mode with empty list", nil, "", true},
		{"empty string mode with explicit list", []string{"inplace"}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac := &AgentConfig{SupportedIsolations: tt.supported}
			got := ac.SupportsIsolation(tt.mode)
			if got != tt.want {
				t.Errorf("SupportsIsolation(%q) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestValidateDefaultIsolationNotInSupported(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "test",
		Agents: map[string]AgentConfig{
			"test": {
				Command:             "test",
				Enabled:             true,
				DefaultIsolation:    "patch",
				SupportedIsolations: []string{"inplace"},
			},
		},
	}
	problems := cfg.Validate()
	found := false
	for _, p := range problems {
		if strings.Contains(p, "default_isolation") && strings.Contains(p, "not in supported_isolations") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected default_isolation/supported_isolations mismatch problem, got %v", problems)
	}
}

func TestValidateDefaultIsolationInSupported(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "test",
		Agents: map[string]AgentConfig{
			"test": {
				Command:             "test",
				Enabled:             true,
				DefaultIsolation:    "inplace",
				SupportedIsolations: []string{"inplace"},
			},
		},
	}
	problems := cfg.Validate()
	for _, p := range problems {
		if strings.Contains(p, "not in supported_isolations") {
			t.Fatalf("unexpected problem: %s", p)
		}
	}
}

func TestValidateInvalidSupportedIsolation(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "test",
		Agents: map[string]AgentConfig{
			"test": {
				Command:             "test",
				Enabled:             true,
				SupportedIsolations: []string{"invalid_mode"},
			},
		},
	}
	problems := cfg.Validate()
	found := false
	for _, p := range problems {
		if strings.Contains(p, "invalid supported_isolations") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected invalid supported_isolations problem, got %v", problems)
	}
}

func TestApplyKnownAgentDefaultsBackfillsSupportedIsolations(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "qoder",
		Agents: map[string]AgentConfig{
			"qoder": {
				Name:    "Qoder CLI",
				Command: "qodercli",
				Enabled: true,
				// No SupportedIsolations set — should be backfilled
			},
		},
	}
	applyKnownAgentDefaults(cfg)
	ac := cfg.Agents["qoder"]
	if len(ac.SupportedIsolations) == 0 {
		t.Fatal("expected SupportedIsolations to be backfilled for qoder")
	}
	if ac.SupportedIsolations[0] != "inplace" {
		t.Fatalf("expected inplace, got %v", ac.SupportedIsolations)
	}
}

func TestApplyKnownAgentDefaultsDoesNotOverrideExistingSupportedIsolations(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "qoder",
		Agents: map[string]AgentConfig{
			"qoder": {
				Name:                "Qoder CLI",
				Command:             "qodercli",
				Enabled:             true,
				SupportedIsolations: []string{"inplace", "worktree"}, // User override
			},
		},
	}
	applyKnownAgentDefaults(cfg)
	ac := cfg.Agents["qoder"]
	if len(ac.SupportedIsolations) != 2 {
		t.Fatalf("expected user's 2-element list preserved, got %v", ac.SupportedIsolations)
	}
}
