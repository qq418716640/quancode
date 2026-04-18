package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	qcontext "github.com/qq418716640/quancode/context"
)

type Config struct {
	DefaultPrimary  string                 `yaml:"default_primary"`
	Agents          map[string]AgentConfig `yaml:"agents"`
	ContextDefaults *qcontext.ContextSpec  `yaml:"context_defaults,omitempty"`
	Preferences     Preferences            `yaml:"preferences,omitempty"`
}

// Preferences contains user-level defaults for delegation behavior.
// CLI flags override these when explicitly set.
type Preferences struct {
	DefaultIsolation     string `yaml:"default_isolation"`      // "inplace" (default), "worktree", "patch"
	FallbackMode         string `yaml:"fallback_mode"`          // "auto" (default), "off"
	SpeculativeDelaySecs int    `yaml:"speculative_delay_secs"` // seconds before launching speculative agent; 0 = disabled (default)
	MinTimeoutSecs       int    `yaml:"min_timeout_secs"`       // floor for effective delegation timeout; 0 = disabled (default)
	DashboardMode        string `yaml:"dashboard_mode"`         // "" = undecided (show tip), "auto" = auto-start on start, "off" = disabled
	DashboardPort        int    `yaml:"dashboard_port"`         // port for dashboard server; 0 or unset = 8377
}

type AgentConfig struct {
	Name         string            `yaml:"name"`
	Command      string            `yaml:"command"`
	Description  string            `yaml:"description"`
	Strengths    []string          `yaml:"strengths"`
	PrimaryArgs  []string          `yaml:"primary_args"`
	DelegateArgs []string          `yaml:"delegate_args"`
	OutputFlag   string            `yaml:"output_flag"`
	TimeoutSecs  int               `yaml:"timeout_secs"`
	Enabled      bool              `yaml:"enabled"`
	Env          map[string]string `yaml:"env"`
	PreferredFor []string          `yaml:"preferred_for"`
	Priority     int               `yaml:"priority"`
	// Data-driven adapter fields
	PromptMode string `yaml:"prompt_mode"` // "append_arg" (default), "stdin", "env", "file"
	PromptFile string `yaml:"prompt_file"` // file name for prompt_mode=file (default: AGENTS.md)
	TaskMode   string `yaml:"task_mode"`   // "arg" (default), "stdin"
	OutputMode string `yaml:"output_mode"` // "stdout" (default), "file"
	// NonInteractiveArgs are extra arguments appended in async mode
	// to ensure the agent runs without interactive prompts.
	NonInteractiveArgs []string `yaml:"non_interactive_args,omitempty"`
	// DefaultIsolation overrides the global preferences.default_isolation for this agent.
	// Use when an agent is incompatible with certain isolation modes (e.g. Qoder + worktree).
	DefaultIsolation string `yaml:"default_isolation,omitempty"`
	// SupportedIsolations declares which isolation modes this agent can use.
	// Empty means all modes are supported (default). Used by speculative
	// parallelism to filter incompatible backup agents, and by delegate
	// to warn when the requested isolation is unsupported.
	SupportedIsolations []string `yaml:"supported_isolations,omitempty"`
	// Context injection config (overrides global ContextDefaults)
	Context *qcontext.ContextSpec `yaml:"context,omitempty"`
	// DiagnosticHints are per-CLI substring→message mappings scanned against
	// stderr+stdout when a delegation fails. Matched hints are printed to
	// stderr to give the user an actionable recovery step (e.g. "copilot
	// logout && login").
	DiagnosticHints []DiagnosticHint `yaml:"diagnostic_hints,omitempty"`
}

// DiagnosticHint is a per-CLI failure pattern and recovery message.
type DiagnosticHint struct {
	Pattern string `yaml:"pattern"` // substring match on stderr+stdout (case-sensitive)
	Hint    string `yaml:"hint"`    // message printed to stderr when matched
}

// SupportsIsolation reports whether the agent supports the given isolation mode.
// Returns true if SupportedIsolations is empty (all modes supported).
func (ac *AgentConfig) SupportsIsolation(mode string) bool {
	if len(ac.SupportedIsolations) == 0 {
		return true
	}
	for _, s := range ac.SupportedIsolations {
		if s == mode {
			return true
		}
	}
	return false
}

// FallbackIsolation returns the best isolation mode to use when the
// requested mode is unsupported. Checks DefaultIsolation, then
// SupportedIsolations[0], then falls back to "inplace".
func (ac *AgentConfig) FallbackIsolation() string {
	if ac.DefaultIsolation != "" {
		return ac.DefaultIsolation
	}
	if len(ac.SupportedIsolations) > 0 {
		return ac.SupportedIsolations[0]
	}
	return "inplace"
}

// Valid enum values for configuration fields.
var (
	validPromptModes   = map[string]bool{"": true, "append_arg": true, "stdin": true, "env": true, "file": true}
	validTaskModes     = map[string]bool{"": true, "arg": true, "stdin": true}
	validOutputModes   = map[string]bool{"": true, "stdout": true, "file": true}
	validIsolationModes = map[string]bool{"": true, "inplace": true, "worktree": true, "patch": true}
	validFallbackModes   = map[string]bool{"": true, "auto": true, "off": true}
	validDashboardModes  = map[string]bool{"": true, "auto": true, "off": true}
)

// Load loads config from the first available source:
// 1. explicit path (--config flag)
// 2. ./quancode.yaml
// 3. ~/.config/quancode/quancode.yaml
// 4. built-in defaults
func Load(explicit string) (*Config, error) {
	// If user explicitly passed --config, it must exist and be valid
	if explicit != "" {
		data, err := os.ReadFile(explicit)
		if err != nil {
			return nil, fmt.Errorf("read config %s: %w", explicit, err)
		}
		var cfg Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", explicit, err)
		}
		applyKnownAgentDefaults(&cfg)
		applyPreferencesDefaults(&cfg.Preferences)
		return &cfg, nil
	}

	paths := []string{}
	paths = append(paths, "quancode.yaml")
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "quancode", "quancode.yaml"))
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cfg Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", p, err)
		}
		applyKnownAgentDefaults(&cfg)
		applyPreferencesDefaults(&cfg.Preferences)
		return &cfg, nil
	}

	// No config file found, use defaults
	return DefaultConfig(), nil
}

// applyKnownAgentDefaults backfills zero-value fields from KnownAgents
// so that user configs only need to specify overrides.
func applyKnownAgentDefaults(cfg *Config) {
	for key, ac := range cfg.Agents {
		def, ok := KnownAgents[key]
		if !ok {
			continue
		}
		if ac.Name == "" {
			ac.Name = def.Name
		}
		if ac.Command == "" {
			ac.Command = def.Command
		}
		if ac.Description == "" {
			ac.Description = def.Description
		}
		if len(ac.Strengths) == 0 && len(def.Strengths) > 0 {
			ac.Strengths = def.Strengths
		}
		if len(ac.PrimaryArgs) == 0 && len(def.PrimaryArgs) > 0 {
			ac.PrimaryArgs = def.PrimaryArgs
		}
		if len(ac.DelegateArgs) == 0 && len(def.DelegateArgs) > 0 {
			ac.DelegateArgs = def.DelegateArgs
		}
		if ac.OutputFlag == "" {
			ac.OutputFlag = def.OutputFlag
		}
		if ac.TimeoutSecs == 0 {
			ac.TimeoutSecs = def.TimeoutSecs
		}
		if len(ac.PreferredFor) == 0 && len(def.PreferredFor) > 0 {
			ac.PreferredFor = def.PreferredFor
		}
		if ac.Priority == 0 {
			ac.Priority = def.Priority
		}
		if ac.PromptMode == "" {
			ac.PromptMode = def.PromptMode
		}
		if ac.PromptFile == "" {
			ac.PromptFile = def.PromptFile
		}
		if ac.TaskMode == "" {
			ac.TaskMode = def.TaskMode
		}
		if ac.OutputMode == "" {
			ac.OutputMode = def.OutputMode
		}
		if ac.DefaultIsolation == "" {
			ac.DefaultIsolation = def.DefaultIsolation
		}
		if len(ac.SupportedIsolations) == 0 && len(def.SupportedIsolations) > 0 {
			ac.SupportedIsolations = def.SupportedIsolations
		}
		if len(ac.NonInteractiveArgs) == 0 && len(def.NonInteractiveArgs) > 0 {
			ac.NonInteractiveArgs = def.NonInteractiveArgs
		}
		if len(ac.DiagnosticHints) == 0 && len(def.DiagnosticHints) > 0 {
			ac.DiagnosticHints = def.DiagnosticHints
		}
		cfg.Agents[key] = ac
	}
}

// DefaultDashboardPort is the default port for the dashboard server.
const DefaultDashboardPort = 8377

// EffectiveDashboardPort returns the configured dashboard port, falling back to DefaultDashboardPort.
func (p *Preferences) EffectiveDashboardPort() int {
	if p.DashboardPort > 0 {
		return p.DashboardPort
	}
	return DefaultDashboardPort
}

// ConfigPath returns the default user config file path.
func ConfigPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "quancode", "quancode.yaml")
	}
	return "quancode.yaml"
}

// Validate checks the config for common issues. Returns a list of problems.
func (c *Config) Validate() []string {
	var problems []string

	if len(c.Agents) == 0 {
		problems = append(problems, "no agents configured")
		return problems
	}

	if c.DefaultPrimary == "" {
		problems = append(problems, "default_primary is not set")
	} else if ac, ok := c.Agents[c.DefaultPrimary]; !ok {
		problems = append(problems, fmt.Sprintf("default_primary %q not found in agents", c.DefaultPrimary))
	} else if !ac.Enabled {
		problems = append(problems, fmt.Sprintf("default_primary %q is disabled", c.DefaultPrimary))
	}

	for key, ac := range c.Agents {
		if ac.Command == "" {
			problems = append(problems, fmt.Sprintf("agent %q: command is empty", key))
		}
		if ac.TimeoutSecs < 0 {
			problems = append(problems, fmt.Sprintf("agent %q: timeout_secs is negative", key))
		}
		if !validPromptModes[ac.PromptMode] {
			problems = append(problems, fmt.Sprintf("agent %q: invalid prompt_mode %q", key, ac.PromptMode))
		}
		if !validTaskModes[ac.TaskMode] {
			problems = append(problems, fmt.Sprintf("agent %q: invalid task_mode %q", key, ac.TaskMode))
		}
		if !validOutputModes[ac.OutputMode] {
			problems = append(problems, fmt.Sprintf("agent %q: invalid output_mode %q", key, ac.OutputMode))
		}
		if !validIsolationModes[ac.DefaultIsolation] {
			problems = append(problems, fmt.Sprintf("agent %q: invalid default_isolation %q", key, ac.DefaultIsolation))
		}
		for _, iso := range ac.SupportedIsolations {
			if !validIsolationModes[iso] {
				problems = append(problems, fmt.Sprintf("agent %q: invalid supported_isolations value %q", key, iso))
			}
		}
		if ac.DefaultIsolation != "" && len(ac.SupportedIsolations) > 0 {
			supported := false
			for _, iso := range ac.SupportedIsolations {
				if iso == ac.DefaultIsolation {
					supported = true
					break
				}
			}
			if !supported {
				problems = append(problems, fmt.Sprintf("agent %q: default_isolation %q not in supported_isolations %v", key, ac.DefaultIsolation, ac.SupportedIsolations))
			}
		}
	}

	// Validate preferences
	if !validIsolationModes[c.Preferences.DefaultIsolation] {
		problems = append(problems, fmt.Sprintf("preferences: invalid default_isolation %q", c.Preferences.DefaultIsolation))
	}
	if !validFallbackModes[c.Preferences.FallbackMode] {
		problems = append(problems, fmt.Sprintf("preferences: invalid fallback_mode %q", c.Preferences.FallbackMode))
	}
	if !validDashboardModes[c.Preferences.DashboardMode] {
		problems = append(problems, fmt.Sprintf("preferences: invalid dashboard_mode %q", c.Preferences.DashboardMode))
	}
	if p := c.Preferences.DashboardPort; p != 0 && (p < 1 || p > 65535) {
		problems = append(problems, fmt.Sprintf("preferences: dashboard_port %d out of range (1-65535)", p))
	}

	return problems
}

// UpdateDashboardMode loads the config from the given explicit path (or default
// search order if empty), sets dashboard_mode, and writes it back to the same
// path. If no config file exists, writes to ConfigPath() with defaults.
func UpdateDashboardMode(explicit, mode string) error {
	if !validDashboardModes[mode] {
		return fmt.Errorf("invalid dashboard_mode %q (valid: auto, off)", mode)
	}

	cfg, err := Load(explicit)
	if err != nil {
		return err
	}
	cfg.Preferences.DashboardMode = mode

	// Determine write target: explicit > first found > default user config.
	cfgPath := explicit
	if cfgPath == "" {
		cfgPath = resolveWritePath()
	}

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(cfgPath, data, 0644)
}

// resolveWritePath returns the first existing config path, or ConfigPath() as fallback.
func resolveWritePath() string {
	paths := []string{"quancode.yaml"}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "quancode", "quancode.yaml"))
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ConfigPath()
}
