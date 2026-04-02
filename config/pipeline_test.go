package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPipeline_DirectPath(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
name: test-pipeline
stages:
  - name: analyze
    agent: claude
    task: "Analyze: {{.Input}}"
  - name: implement
    task: "Implement based on: {{.Prev.Output}}"
    verify:
      - "go test ./..."
    verify_strict: true
on_failure: stop
`
	path := filepath.Join(dir, "test.yaml")
	os.WriteFile(path, []byte(yamlContent), 0644)

	def, err := LoadPipeline(path)
	if err != nil {
		t.Fatalf("LoadPipeline: %v", err)
	}
	if def.Name != "test-pipeline" {
		t.Errorf("name = %q, want %q", def.Name, "test-pipeline")
	}
	if len(def.Stages) != 2 {
		t.Fatalf("stages = %d, want 2", len(def.Stages))
	}
	if def.Stages[0].Agent != "claude" {
		t.Errorf("stage[0].agent = %q, want %q", def.Stages[0].Agent, "claude")
	}
	if def.Stages[1].VerifyStrict != true {
		t.Error("stage[1].verify_strict should be true")
	}
	if def.OnFailure != "stop" {
		t.Errorf("on_failure = %q, want %q", def.OnFailure, "stop")
	}
}

func TestLoadPipeline_ByName(t *testing.T) {
	dir := t.TempDir()
	pipelinesDir := filepath.Join(dir, ".quancode", "pipelines")
	os.MkdirAll(pipelinesDir, 0755)

	yamlContent := `
name: my-flow
stages:
  - name: step1
    task: "do it"
`
	os.WriteFile(filepath.Join(pipelinesDir, "my-flow.yaml"), []byte(yamlContent), 0644)

	// Change to the temp dir so .quancode/pipelines/ is found
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	def, err := LoadPipeline("my-flow")
	if err != nil {
		t.Fatalf("LoadPipeline by name: %v", err)
	}
	if def.Name != "my-flow" {
		t.Errorf("name = %q, want %q", def.Name, "my-flow")
	}
}

func TestLoadPipeline_NotFound(t *testing.T) {
	_, err := LoadPipeline("nonexistent-pipeline-xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent pipeline")
	}
}

func TestPipelineDef_Validate_Valid(t *testing.T) {
	def := &PipelineDef{
		Name: "test",
		Stages: []StageDef{
			{Name: "a", Task: "do A"},
			{Name: "b", Task: "do B"},
		},
	}
	if problems := def.Validate(nil); len(problems) != 0 {
		t.Errorf("unexpected problems: %v", problems)
	}
}

func TestPipelineDef_Validate_Errors(t *testing.T) {
	tests := []struct {
		name    string
		def     PipelineDef
		wantMin int // minimum number of problems
	}{
		{
			name:    "empty name",
			def:     PipelineDef{Stages: []StageDef{{Name: "a", Task: "t"}}},
			wantMin: 1,
		},
		{
			name:    "no stages",
			def:     PipelineDef{Name: "p"},
			wantMin: 1,
		},
		{
			name: "duplicate stage names",
			def: PipelineDef{
				Name: "p",
				Stages: []StageDef{
					{Name: "dup", Task: "t1"},
					{Name: "dup", Task: "t2"},
				},
			},
			wantMin: 1,
		},
		{
			name: "empty stage name",
			def: PipelineDef{
				Name:   "p",
				Stages: []StageDef{{Task: "t"}},
			},
			wantMin: 1,
		},
		{
			name: "empty task",
			def: PipelineDef{
				Name:   "p",
				Stages: []StageDef{{Name: "a"}},
			},
			wantMin: 1,
		},
		{
			name: "invalid template",
			def: PipelineDef{
				Name:   "p",
				Stages: []StageDef{{Name: "a", Task: "{{.bad template"}},
			},
			wantMin: 1,
		},
		{
			name: "invalid on_failure",
			def: PipelineDef{
				Name:      "p",
				OnFailure: "explode",
				Stages:    []StageDef{{Name: "a", Task: "t"}},
			},
			wantMin: 1,
		},
		{
			name: "negative timeout",
			def: PipelineDef{
				Name:   "p",
				Stages: []StageDef{{Name: "a", Task: "t", TimeoutSecs: -1}},
			},
			wantMin: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := tt.def.Validate(nil)
			if len(problems) < tt.wantMin {
				t.Errorf("got %d problems, want at least %d", len(problems), tt.wantMin)
			}
		})
	}
}

func TestPipelineDef_Validate_AgentRef(t *testing.T) {
	cfg := &Config{
		Agents: map[string]AgentConfig{
			"claude": {Enabled: true, Command: "claude"},
			"off":    {Enabled: false, Command: "off"},
		},
	}

	def := &PipelineDef{
		Name: "p",
		Stages: []StageDef{
			{Name: "ok", Task: "t", Agent: "claude"},
			{Name: "bad", Task: "t", Agent: "nonexistent"},
			{Name: "disabled", Task: "t", Agent: "off"},
		},
	}
	problems := def.Validate(cfg)
	if len(problems) != 2 {
		t.Errorf("got %d problems, want 2: %v", len(problems), problems)
	}
}

func TestPipelineDef_ResolveOnFailure(t *testing.T) {
	def := &PipelineDef{OnFailure: "continue"}

	// Stage override
	if got := def.ResolveOnFailure(StageDef{OnFailure: "stop"}); got != "stop" {
		t.Errorf("stage override: got %q, want %q", got, "stop")
	}
	// Pipeline fallback
	if got := def.ResolveOnFailure(StageDef{}); got != "continue" {
		t.Errorf("pipeline fallback: got %q, want %q", got, "continue")
	}
	// Default
	def.OnFailure = ""
	if got := def.ResolveOnFailure(StageDef{}); got != "stop" {
		t.Errorf("default: got %q, want %q", got, "stop")
	}
}
