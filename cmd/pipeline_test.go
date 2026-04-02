package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderStageTask(t *testing.T) {
	pctx := &pipelineContext{
		Input: "fix the bug",
		Stages: map[string]*stageResult{
			"analyze": {
				Output:       "Root cause: nil pointer",
				ChangedFiles: []string{"main.go", "util.go"},
			},
		},
	}
	pctx.Prev = pctx.Stages["analyze"]

	tests := []struct {
		name    string
		tmpl    string
		want    string
		wantErr bool
	}{
		{
			name: "input",
			tmpl: "Task: {{.Input}}",
			want: "Task: fix the bug",
		},
		{
			name: "prev output",
			tmpl: "Based on: {{.Prev.Output}}",
			want: "Based on: Root cause: nil pointer",
		},
		{
			name: "stage reference",
			tmpl: "Analysis: {{.Stages.analyze.Output}}",
			want: "Analysis: Root cause: nil pointer",
		},
		{
			name: "changed files range",
			tmpl: `Files:{{range .Stages.analyze.ChangedFiles}} {{.}}{{end}}`,
			want: "Files: main.go util.go",
		},
		{
			name:    "missing stage errors",
			tmpl:    "{{.Stages.nonexistent.Output}}",
			wantErr: true,
		},
		{
			name:    "bad template syntax",
			tmpl:    "{{.bad template",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderStageTask(tt.tmpl, pctx)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderStageTask_FirstStageNilPrev(t *testing.T) {
	pctx := &pipelineContext{
		Input:  "hello",
		Stages: map[string]*stageResult{},
	}
	// First stage: Prev is nil, template should only use .Input
	got, err := renderStageTask("Task: {{.Input}}", pctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Task: hello" {
		t.Errorf("got %q, want %q", got, "Task: hello")
	}
}

func TestPipelineCmd_DryRun(t *testing.T) {
	isolateHome(t)

	// Create a pipeline file
	dir := t.TempDir()
	pipelineYAML := `
name: test-pipe
stages:
  - name: step1
    agent: echo-agent
    task: "Do {{.Input}}"
  - name: step2
    task: "Continue from: {{.Prev.Output}}"
    verify:
      - "echo ok"
    verify_strict: true
    on_failure: continue
`
	pipelinePath := filepath.Join(dir, "test-pipe.yaml")
	os.WriteFile(pipelinePath, []byte(pipelineYAML), 0644)

	cfgPath := writeConfig(t, dir, `
default_primary: echo-agent
agents:
  echo-agent:
    name: Echo
    command: echo
    enabled: true
    delegate_args:
      - -c
    priority: 10
`)

	oldCfgFile := cfgFile
	oldFormat := pipelineFormat
	oldDryRun := pipelineDryRun
	cfgFile = cfgPath
	pipelineFormat = "text"
	pipelineDryRun = true
	defer func() {
		cfgFile = oldCfgFile
		pipelineFormat = oldFormat
		pipelineDryRun = oldDryRun
	}()

	err := pipelineCmd.RunE(pipelineCmd, []string{pipelinePath, "something"})
	if err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}
}

func TestPipelineCmd_Integration(t *testing.T) {
	isolateHome(t)

	// Create a git repo for worktree isolation
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "dummy.txt"), []byte("hello"), 0644)
	runGitCmd(t, dir, "add", "-A")
	runGitCmd(t, dir, "commit", "-m", "init")

	// Pipeline: stage1 echoes something, stage2 references stage1 output
	pipelineYAML := `
name: echo-pipe
stages:
  - name: greet
    agent: sh-agent
    task: "greet the user"
  - name: respond
    agent: sh-agent
    task: "respond to greeting"
`
	pipelinePath := filepath.Join(dir, "echo-pipe.yaml")
	os.WriteFile(pipelinePath, []byte(pipelineYAML), 0644)

	cfgPath := writeConfig(t, dir, `
default_primary: sh-agent
agents:
  sh-agent:
    name: Shell Echo
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - printf stage-output
    priority: 10
`)

	oldCfgFile := cfgFile
	oldFormat := pipelineFormat
	oldIsolation := pipelineIsolation
	oldWorkdir := pipelineWorkdir
	oldNoContext := pipelineNoContext
	oldDryRun := pipelineDryRun
	cfgFile = cfgPath
	pipelineFormat = "json"
	pipelineIsolation = "worktree"
	pipelineWorkdir = dir
	pipelineNoContext = true
	pipelineDryRun = false
	defer func() {
		cfgFile = oldCfgFile
		pipelineFormat = oldFormat
		pipelineIsolation = oldIsolation
		pipelineWorkdir = oldWorkdir
		pipelineNoContext = oldNoContext
		pipelineDryRun = oldDryRun
	}()

	out := captureStdout(t, func() {
		err := pipelineCmd.RunE(pipelineCmd, []string{pipelinePath, "hello"})
		if err != nil {
			t.Fatalf("pipeline returned error: %v", err)
		}
	})

	var result PipelineResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse JSON output: %v\nraw: %s", err, out)
	}

	if result.Pipeline != "echo-pipe" {
		t.Errorf("pipeline = %q, want %q", result.Pipeline, "echo-pipe")
	}
	if result.Status != StatusCompleted {
		t.Errorf("status = %q, want %q", result.Status, StatusCompleted)
	}
	if len(result.Stages) != 2 {
		t.Fatalf("stages = %d, want 2", len(result.Stages))
	}
	for i, s := range result.Stages {
		if s.Status != StatusCompleted {
			t.Errorf("stage[%d] status = %q, want %q", i, s.Status, StatusCompleted)
		}
		if s.Agent != "sh-agent" {
			t.Errorf("stage[%d] agent = %q, want %q", i, s.Agent, "sh-agent")
		}
	}
	if !strings.HasPrefix(result.PipelineID, "pipe_") {
		t.Errorf("pipeline_id = %q, want pipe_ prefix", result.PipelineID)
	}
}

func TestPipelineCmd_StageFailureStop(t *testing.T) {
	isolateHome(t)

	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "dummy.txt"), []byte("x"), 0644)
	runGitCmd(t, dir, "add", "-A")
	runGitCmd(t, dir, "commit", "-m", "init")

	// Stage1 fails (exit 1), on_failure=stop → stage2 should not run
	pipelineYAML := `
name: fail-pipe
on_failure: stop
stages:
  - name: fail-stage
    agent: fail-agent
    task: "fail"
  - name: never-stage
    agent: fail-agent
    task: "should not run"
`
	pipelinePath := filepath.Join(dir, "fail-pipe.yaml")
	os.WriteFile(pipelinePath, []byte(pipelineYAML), 0644)

	cfgPath := writeConfig(t, dir, `
default_primary: fail-agent
agents:
  fail-agent:
    name: Failing Agent
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - "echo fail-output; exit 1"
    priority: 10
`)

	oldCfgFile := cfgFile
	oldFormat := pipelineFormat
	oldIsolation := pipelineIsolation
	oldWorkdir := pipelineWorkdir
	oldNoContext := pipelineNoContext
	oldDryRun := pipelineDryRun
	cfgFile = cfgPath
	pipelineFormat = "json"
	pipelineIsolation = "worktree"
	pipelineWorkdir = dir
	pipelineNoContext = true
	pipelineDryRun = false
	defer func() {
		cfgFile = oldCfgFile
		pipelineFormat = oldFormat
		pipelineIsolation = oldIsolation
		pipelineWorkdir = oldWorkdir
		pipelineNoContext = oldNoContext
		pipelineDryRun = oldDryRun
	}()

	out := captureStdout(t, func() {
		err := pipelineCmd.RunE(pipelineCmd, []string{pipelinePath})
		if err == nil {
			t.Fatal("expected error for failing pipeline")
		}
	})

	var result PipelineResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse JSON: %v\nraw: %s", err, out)
	}

	if result.Status != StatusFailed {
		t.Errorf("status = %q, want %q", result.Status, StatusFailed)
	}
	// Only 1 stage should have run (stop on failure)
	if len(result.Stages) != 1 {
		t.Errorf("stages = %d, want 1 (stopped after failure)", len(result.Stages))
	}
}

func TestPipelineCmd_StageFailureContinue(t *testing.T) {
	isolateHome(t)

	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "dummy.txt"), []byte("x"), 0644)
	runGitCmd(t, dir, "add", "-A")
	runGitCmd(t, dir, "commit", "-m", "init")

	// Stage1 fails but on_failure=continue → stage2 should still run
	pipelineYAML := `
name: continue-pipe
on_failure: continue
stages:
  - name: fail-stage
    agent: fail-agent
    task: "fail"
  - name: ok-stage
    agent: ok-agent
    task: "succeed"
`
	pipelinePath := filepath.Join(dir, "continue-pipe.yaml")
	os.WriteFile(pipelinePath, []byte(pipelineYAML), 0644)

	cfgPath := writeConfig(t, dir, `
default_primary: ok-agent
agents:
  fail-agent:
    name: Failing
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - "exit 1"
    priority: 5
  ok-agent:
    name: OK
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - printf ok-output
    priority: 10
`)

	oldCfgFile := cfgFile
	oldFormat := pipelineFormat
	oldIsolation := pipelineIsolation
	oldWorkdir := pipelineWorkdir
	oldNoContext := pipelineNoContext
	oldDryRun := pipelineDryRun
	cfgFile = cfgPath
	pipelineFormat = "json"
	pipelineIsolation = "worktree"
	pipelineWorkdir = dir
	pipelineNoContext = true
	pipelineDryRun = false
	defer func() {
		cfgFile = oldCfgFile
		pipelineFormat = oldFormat
		pipelineIsolation = oldIsolation
		pipelineWorkdir = oldWorkdir
		pipelineNoContext = oldNoContext
		pipelineDryRun = oldDryRun
	}()

	out := captureStdout(t, func() {
		err := pipelineCmd.RunE(pipelineCmd, []string{pipelinePath})
		if err == nil {
			t.Fatal("expected error for pipeline with failures")
		}
	})

	var result PipelineResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("parse JSON: %v\nraw: %s", err, out)
	}

	// Both stages should have run
	if len(result.Stages) != 2 {
		t.Fatalf("stages = %d, want 2", len(result.Stages))
	}
	if result.Stages[0].Status != StatusFailed {
		t.Errorf("stage[0] status = %q, want %q", result.Stages[0].Status, StatusFailed)
	}
	if result.Stages[1].Status != StatusCompleted {
		t.Errorf("stage[1] status = %q, want %q", result.Stages[1].Status, StatusCompleted)
	}
}

func TestAppendUnique(t *testing.T) {
	got := appendUnique([]string{"a", "b"}, []string{"b", "c", "a", "d"})
	want := []string{"a", "b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
