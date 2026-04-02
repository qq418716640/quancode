package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// PipelineDef defines a pipeline of ordered delegation stages.
type PipelineDef struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description,omitempty"`
	Stages      []StageDef `yaml:"stages"`
	OnFailure   string     `yaml:"on_failure,omitempty"`
	Verify      []string   `yaml:"verify,omitempty"`
	VerifyStrict bool      `yaml:"verify_strict,omitempty"`
}

// StageDef defines a single stage within a pipeline.
type StageDef struct {
	Name         string   `yaml:"name"`
	Agent        string   `yaml:"agent,omitempty"`
	Task         string   `yaml:"task"`
	OnFailure    string   `yaml:"on_failure,omitempty"`
	TimeoutSecs  int      `yaml:"timeout_secs,omitempty"`
	Verify       []string `yaml:"verify,omitempty"`
	VerifyStrict bool     `yaml:"verify_strict,omitempty"`
}

var validOnFailureModes = map[string]bool{
	"": true, "stop": true, "continue": true,
}

// LoadPipeline loads a pipeline definition by name or file path.
// Search order: direct path → .quancode/pipelines/{name}.yaml → ~/.config/quancode/pipelines/{name}.yaml
func LoadPipeline(nameOrPath string) (*PipelineDef, error) {
	data, err := findPipelineFile(nameOrPath)
	if err != nil {
		return nil, err
	}

	var def PipelineDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse pipeline: %w", err)
	}

	return &def, nil
}

func findPipelineFile(nameOrPath string) ([]byte, error) {
	// Direct path: contains separator or ends with .yaml/.yml
	if strings.ContainsRune(nameOrPath, filepath.Separator) ||
		strings.HasSuffix(nameOrPath, ".yaml") ||
		strings.HasSuffix(nameOrPath, ".yml") {
		data, err := os.ReadFile(nameOrPath)
		if err != nil {
			return nil, fmt.Errorf("read pipeline file %s: %w", nameOrPath, err)
		}
		return data, nil
	}

	// Search by name
	searchPaths := []string{
		filepath.Join(".quancode", "pipelines", nameOrPath+".yaml"),
	}
	if home, err := os.UserHomeDir(); err == nil {
		searchPaths = append(searchPaths,
			filepath.Join(home, ".config", "quancode", "pipelines", nameOrPath+".yaml"),
		)
	}

	for _, p := range searchPaths {
		data, err := os.ReadFile(p)
		if err == nil {
			return data, nil
		}
	}

	return nil, fmt.Errorf("pipeline %q not found (searched: %s)", nameOrPath, strings.Join(searchPaths, ", "))
}

// Validate checks the pipeline definition for errors.
// cfg is used to validate agent references; pass nil to skip agent checks.
func (p *PipelineDef) Validate(cfg *Config) []string {
	var problems []string

	if p.Name == "" {
		problems = append(problems, "pipeline name is empty")
	}
	if len(p.Stages) == 0 {
		problems = append(problems, "pipeline has no stages")
	}

	if !validOnFailureModes[p.OnFailure] {
		problems = append(problems, fmt.Sprintf("invalid on_failure %q (must be stop or continue)", p.OnFailure))
	}

	seen := make(map[string]bool)
	for i, s := range p.Stages {
		prefix := fmt.Sprintf("stage[%d]", i)

		if s.Name == "" {
			problems = append(problems, prefix+": name is empty")
		} else if seen[s.Name] {
			problems = append(problems, fmt.Sprintf("%s: duplicate name %q", prefix, s.Name))
		} else {
			seen[s.Name] = true
			prefix = fmt.Sprintf("stage %q", s.Name)
		}

		if s.Task == "" {
			problems = append(problems, prefix+": task is empty")
		} else {
			// Validate template syntax
			if _, err := template.New("").Option("missingkey=error").Parse(s.Task); err != nil {
				problems = append(problems, fmt.Sprintf("%s: invalid task template: %v", prefix, err))
			}
		}

		if !validOnFailureModes[s.OnFailure] {
			problems = append(problems, fmt.Sprintf("%s: invalid on_failure %q", prefix, s.OnFailure))
		}

		if s.TimeoutSecs < 0 {
			problems = append(problems, fmt.Sprintf("%s: timeout_secs is negative", prefix))
		}

		// Validate agent reference
		if cfg != nil && s.Agent != "" {
			if ac, ok := cfg.Agents[s.Agent]; !ok {
				problems = append(problems, fmt.Sprintf("%s: unknown agent %q", prefix, s.Agent))
			} else if !ac.Enabled {
				problems = append(problems, fmt.Sprintf("%s: agent %q is disabled", prefix, s.Agent))
			}
		}
	}

	return problems
}

// ResolveOnFailure returns the effective on_failure policy for a stage,
// falling back to the pipeline-level setting, then to "stop".
func (p *PipelineDef) ResolveOnFailure(stage StageDef) string {
	if stage.OnFailure != "" {
		return stage.OnFailure
	}
	if p.OnFailure != "" {
		return p.OnFailure
	}
	return "stop"
}
