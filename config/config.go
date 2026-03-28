package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DefaultPrimary string                 `yaml:"default_primary"`
	Agents         map[string]AgentConfig `yaml:"agents"`
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
}

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
		return &cfg, nil
	}

	// No config file found, use defaults
	return DefaultConfig(), nil
}

// applyKnownAgentDefaults backfills newer adapter fields for known agents
// when loading older configs generated before those fields existed.
func applyKnownAgentDefaults(cfg *Config) {
	for key, ac := range cfg.Agents {
		def, ok := KnownAgents[key]
		if !ok {
			continue
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
		cfg.Agents[key] = ac
	}
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
	}

	return problems
}
