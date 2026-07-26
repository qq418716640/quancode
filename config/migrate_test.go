package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMigrateDeprecatedDefaultsUpgradesExactMatch(t *testing.T) {
	cfg := &Config{Agents: map[string]AgentConfig{
		"codex":  {Command: "codex", DelegateArgs: []string{"exec", "--full-auto", "--ephemeral"}},
		"claude": {Command: "claude", DelegateArgs: []string{"--dangerously-skip-permissions", "-p", "--output-format", "text"}},
	}}
	migrateDeprecatedDefaults(cfg)

	if got := cfg.Agents["codex"].DelegateArgs; !slices.Equal(got, KnownAgents["codex"].DelegateArgs) {
		t.Errorf("codex DelegateArgs = %v, want %v", got, KnownAgents["codex"].DelegateArgs)
	}
	if got := cfg.Agents["claude"].DelegateArgs; !slices.Equal(got, KnownAgents["claude"].DelegateArgs) {
		t.Errorf("claude DelegateArgs = %v, want %v", got, KnownAgents["claude"].DelegateArgs)
	}
}

// The whole point of matching byte-for-byte: a user who touched the list at
// all owns it, and an implicit migration must not silently undo their edit.
func TestMigrateDeprecatedDefaultsLeavesCustomizedArgsAlone(t *testing.T) {
	customized := [][]string{
		{"exec", "--full-auto"},                             // flag removed
		{"exec", "--full-auto", "--ephemeral", "--verbose"}, // flag added
		{"exec", "--ephemeral", "--full-auto"},              // reordered
		{"exec", "--sandbox", "read-only", "--ephemeral"},   // different sandbox
	}
	for _, args := range customized {
		cfg := &Config{Agents: map[string]AgentConfig{
			"codex": {Command: "codex", DelegateArgs: slices.Clone(args)},
		}}
		migrateDeprecatedDefaults(cfg)
		if got := cfg.Agents["codex"].DelegateArgs; !slices.Equal(got, args) {
			t.Errorf("customized %v was overwritten with %v", args, got)
		}
	}
}

// A config pointing the agent key at a pinned or forked binary is not the
// config these migrations were written for; new flags could break it.
func TestMigrateDeprecatedDefaultsSkipsNonStockCommand(t *testing.T) {
	prior := []string{"exec", "--full-auto", "--ephemeral"}
	cfg := &Config{Agents: map[string]AgentConfig{
		"codex": {Command: "/opt/codex-0.90/codex", DelegateArgs: prior},
	}}
	migrateDeprecatedDefaults(cfg)

	if got := cfg.Agents["codex"].DelegateArgs; !slices.Equal(got, prior) {
		t.Errorf("pinned binary was migrated to %v", got)
	}
}

// codex's --output-last-message is now actively wrong: under output_mode:
// file the runner would overwrite the JSONL stream with plain text, leaving
// nothing to parse. Dropping it from KnownAgents does not reach a config
// that already wrote it to disk, so the migration must clear it.
func TestMigrateClearsDeprecatedOutputFlag(t *testing.T) {
	cfg := &Config{Agents: map[string]AgentConfig{
		"codex": {Command: "codex", OutputFlag: "--output-last-message"},
	}}
	migrateDeprecatedDefaults(cfg)

	if got := cfg.Agents["codex"].OutputFlag; got != "" {
		t.Errorf("OutputFlag = %q, want cleared", got)
	}
}

func TestMigrateKeepsCustomOutputFlag(t *testing.T) {
	cfg := &Config{Agents: map[string]AgentConfig{
		"codex": {Command: "codex", OutputFlag: "--my-own-flag"},
	}}
	migrateDeprecatedDefaults(cfg)

	if got := cfg.Agents["codex"].OutputFlag; got != "--my-own-flag" {
		t.Errorf("OutputFlag = %q, want untouched", got)
	}
}

func TestMigrateDeprecatedDefaultsIgnoresUnknownAgents(t *testing.T) {
	cfg := &Config{Agents: map[string]AgentConfig{
		"mycli": {Command: "codex", DelegateArgs: []string{"exec", "--full-auto", "--ephemeral"}},
	}}
	migrateDeprecatedDefaults(cfg)

	want := []string{"exec", "--full-auto", "--ephemeral"}
	if got := cfg.Agents["mycli"].DelegateArgs; !slices.Equal(got, want) {
		t.Errorf("unrelated agent mutated to %v", got)
	}
}

// End-to-end through Load: a config file written by an older `quancode init`
// must come back with the current flags, since applyKnownAgentDefaults only
// fills empty fields and would never touch a persisted non-empty list.
func TestLoadMigratesPersistedDeprecatedArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quancode.yaml")
	body := `default_primary: claude
agents:
  claude:
    command: claude
    delegate_args: ["--dangerously-skip-permissions", "-p", "--output-format", "text"]
    enabled: true
  codex:
    command: codex
    delegate_args: ["exec", "--full-auto", "--ephemeral"]
    enabled: true
`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	codex := cfg.Agents["codex"]
	if slices.Contains(codex.DelegateArgs, "--full-auto") {
		t.Errorf("deprecated --full-auto survived Load: %v", codex.DelegateArgs)
	}
	if !slices.Contains(codex.DelegateArgs, "--json") {
		t.Errorf("codex DelegateArgs = %v, want --json", codex.DelegateArgs)
	}
	if codex.ResultFormat != "jsonl_events" {
		t.Errorf("codex ResultFormat = %q, want jsonl_events", codex.ResultFormat)
	}

	claude := cfg.Agents["claude"]
	if slices.Contains(claude.DelegateArgs, "text") {
		t.Errorf("claude still on --output-format text: %v", claude.DelegateArgs)
	}
	if claude.ResultFormat != "json_object" {
		t.Errorf("claude ResultFormat = %q, want json_object", claude.ResultFormat)
	}
}

// A user who customized DelegateArgs keeps them — and must NOT get a
// ResultFormat that assumes flags they never passed. Arming the parser
// anyway would let a task whose own answer happens to look like the
// envelope be silently rewritten, with fabricated token counts attached.
func TestLoadDoesNotBackfillResultFormatOntoCustomArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quancode.yaml")
	custom := []string{"-p", "--output-format", "text", "--model", "opus"}
	body := `default_primary: claude
agents:
  claude:
    command: claude
    delegate_args: ["-p", "--output-format", "text", "--model", "opus"]
    enabled: true
`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	claude := cfg.Agents["claude"]
	if !slices.Equal(claude.DelegateArgs, custom) {
		t.Fatalf("custom args were overwritten: %v", claude.DelegateArgs)
	}
	if claude.ResultFormat != "" {
		t.Errorf("ResultFormat = %q, want empty (parser must stay disarmed)", claude.ResultFormat)
	}
}

// An explicit result_format is the user's own opt-in and survives even on
// custom args — the backfill guard restricts inference, not intent.
func TestLoadKeepsExplicitResultFormatOnCustomArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quancode.yaml")
	body := `default_primary: claude
agents:
  claude:
    command: claude
    delegate_args: ["-p", "--output-format", "json", "--model", "opus"]
    result_format: json_object
    enabled: true
`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Agents["claude"].ResultFormat; got != "json_object" {
		t.Errorf("ResultFormat = %q, want json_object", got)
	}
}

// v0.1.0 shipped claude without --dangerously-skip-permissions; a config
// persisted back then must still reach the JSON flags, or it silently keeps
// text output forever while every newer install collects cost data.
func TestLoadMigratesOldestClaudeDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quancode.yaml")
	body := `default_primary: claude
agents:
  claude:
    command: claude
    delegate_args: ["-p", "--output-format", "text"]
    enabled: true
`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	claude := cfg.Agents["claude"]
	if !slices.Equal(claude.DelegateArgs, KnownAgents["claude"].DelegateArgs) {
		t.Errorf("DelegateArgs = %v, want the current default", claude.DelegateArgs)
	}
	if claude.ResultFormat != "json_object" {
		t.Errorf("ResultFormat = %q, want json_object after migration", claude.ResultFormat)
	}
}

func TestValidateRejectsUnknownResultFormat(t *testing.T) {
	cfg := &Config{
		DefaultPrimary: "claude",
		Agents: map[string]AgentConfig{
			"claude": {Command: "claude", Enabled: true, ResultFormat: "jsonl_event"},
		},
	}
	problems := cfg.Validate()
	if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, "result_format") }) {
		t.Errorf("Validate() = %v, want a result_format complaint", problems)
	}
}
