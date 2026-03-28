package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/qq418716640/quancode/agent"
)

func TestStartRunEReturnsPrimaryExitStatusAndRestoresPromptFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: test
agents:
  test:
    name: Test Agent
    command: /bin/sh
    enabled: true
    prompt_mode: file
    prompt_file: AGENTS.md
    primary_args:
      - -c
      - exit 7
`)
	agentsPath := filepath.Join(dir, "AGENTS.md")
	writeTestFile(t, agentsPath, "original\n")

	oldCfgFile := cfgFile
	oldPrimary := primaryAgent
	cfgFile = cfgPath
	primaryAgent = ""
	defer func() {
		cfgFile = oldCfgFile
		primaryAgent = oldPrimary
	}()

	var gotErr error
	withWorkingDir(t, dir, func() {
		gotErr = startCmd.RunE(startCmd, nil)
	})

	var exitErr *agent.ExitStatusError
	if !errors.As(gotErr, &exitErr) {
		t.Fatalf("expected ExitStatusError, got %v", gotErr)
	}
	if exitErr.Code != 7 {
		t.Fatalf("expected exit code 7, got %d", exitErr.Code)
	}

	data, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read restored AGENTS.md: %v", err)
	}
	if string(data) != "original\n" {
		t.Fatalf("expected AGENTS.md to be restored, got %q", string(data))
	}
}
