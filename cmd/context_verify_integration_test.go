package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qq418716640/quancode/agent"
)

// --- Context injection tests ---

func TestDelegateContextInjectsClaudeMD(t *testing.T) {
	dir := t.TempDir()
	// Create a CLAUDE.md that the agent will see in its task
	writeTestFile(t, filepath.Join(dir, "CLAUDE.md"), "build: go test ./...")

	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  echoer:
    name: Echoer
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - cat
    task_mode: stdin
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldNoContext := delegateNoContext
	cfgFile = cfgPath
	delegateAgent = "echoer"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "inplace"
	delegateNoContext = false
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		delegateNoContext = oldNoContext
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"do something"}); err != nil {
			t.Fatalf("delegate RunE error: %v", err)
		}
	})

	var got DelegationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\nout=%q", err, out)
	}
	if !strings.Contains(got.Output, "PROJECT CONTEXT") {
		t.Fatalf("expected context header in output, got %q", got.Output)
	}
	if !strings.Contains(got.Output, "build: go test") {
		t.Fatalf("expected CLAUDE.md content in output, got %q", got.Output)
	}
	if !strings.Contains(got.Output, "=== TASK ===") {
		t.Fatalf("expected task separator in output, got %q", got.Output)
	}
	if !strings.Contains(got.Output, "do something") {
		t.Fatalf("expected original task in output, got %q", got.Output)
	}
}

func TestDelegateNoContextSkipsInjection(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "CLAUDE.md"), "should not appear")

	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  echoer:
    name: Echoer
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - cat
    task_mode: stdin
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldNoContext := delegateNoContext
	cfgFile = cfgPath
	delegateAgent = "echoer"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "inplace"
	delegateNoContext = true
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		delegateNoContext = oldNoContext
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"do something"}); err != nil {
			t.Fatalf("delegate RunE error: %v", err)
		}
	})

	var got DelegationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\nout=%q", err, out)
	}
	if strings.Contains(got.Output, "PROJECT CONTEXT") {
		t.Fatalf("expected no context injection with --no-context, got %q", got.Output)
	}
	if got.Output != "do something" {
		t.Fatalf("expected raw task only, got %q", got.Output)
	}
}

func TestDelegateContextFilesInjection(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main")

	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  echoer:
    name: Echoer
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - cat
    task_mode: stdin
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldNoContext := delegateNoContext
	oldCtxFiles := delegateContextFiles
	cfgFile = cfgPath
	delegateAgent = "echoer"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "inplace"
	delegateNoContext = false
	delegateContextFiles = []string{"main.go"}
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		delegateNoContext = oldNoContext
		delegateContextFiles = oldCtxFiles
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"review"}); err != nil {
			t.Fatalf("delegate RunE error: %v", err)
		}
	})

	var got DelegationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\nout=%q", err, out)
	}
	if !strings.Contains(got.Output, "package main") {
		t.Fatalf("expected context file content, got %q", got.Output)
	}
	if !strings.Contains(got.Output, "main.go") {
		t.Fatalf("expected context file path, got %q", got.Output)
	}
}

// --- Verify integration tests ---

func TestDelegateVerifyPassInplace(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  worker:
    name: Worker
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - echo done
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldVerify := delegateVerify
	oldVerifyStrict := delegateVerifyStrict
	oldVerifyTimeout := delegateVerifyTimeout
	oldNoContext := delegateNoContext
	cfgFile = cfgPath
	delegateAgent = "worker"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "inplace"
	delegateVerify = []string{"true"}
	delegateVerifyStrict = nil
	delegateVerifyTimeout = 10
	delegateNoContext = true
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		delegateVerify = oldVerify
		delegateVerifyStrict = oldVerifyStrict
		delegateVerifyTimeout = oldVerifyTimeout
		delegateNoContext = oldNoContext
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"task"}); err != nil {
			t.Fatalf("error: %v", err)
		}
	})

	var got DelegationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\nout=%q", err, out)
	}
	if got.Status != StatusCompleted {
		t.Fatalf("expected completed, got %q", got.Status)
	}
	if got.Verify == nil {
		t.Fatal("expected verify result")
	}
	if got.Verify.Status != VerifyPassed {
		t.Fatalf("expected verify passed, got %q", got.Verify.Status)
	}
}

func TestDelegateVerifyStrictFailInplace(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  worker:
    name: Worker
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - echo done
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldVerify := delegateVerify
	oldVerifyStrict := delegateVerifyStrict
	oldVerifyTimeout := delegateVerifyTimeout
	oldNoContext := delegateNoContext
	cfgFile = cfgPath
	delegateAgent = "worker"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "inplace"
	delegateVerify = nil
	delegateVerifyStrict = []string{"false"} // `false` command exits 1
	delegateVerifyTimeout = 10
	delegateNoContext = true
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		delegateVerify = oldVerify
		delegateVerifyStrict = oldVerifyStrict
		delegateVerifyTimeout = oldVerifyTimeout
		delegateNoContext = oldNoContext
	}()

	var gotErr error
	out := captureStdout(t, func() {
		gotErr = delegateCmd.RunE(delegateCmd, []string{"task"})
	})

	var exitErr *agent.ExitStatusError
	if !errors.As(gotErr, &exitErr) {
		t.Fatalf("expected ExitStatusError, got %v", gotErr)
	}

	var got DelegationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\nout=%q", err, out)
	}
	if got.Status != StatusFailed {
		t.Fatalf("expected failed, got %q", got.Status)
	}
	if got.Verify == nil || got.Verify.Status != VerifyFailed {
		t.Fatalf("expected verify failed, got %+v", got.Verify)
	}
}

func TestDelegateVerifyStrictFailWorktreeDoesNotApply(t *testing.T) {
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.name", "Test")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	writeTestFile(t, filepath.Join(dir, "file.txt"), "original\n")
	runGitCmd(t, dir, "add", "file.txt")
	runGitCmd(t, dir, "commit", "-m", "init")

	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  worker:
    name: Worker
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - printf "modified\n" >> file.txt
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldVerify := delegateVerify
	oldVerifyStrict := delegateVerifyStrict
	oldVerifyTimeout := delegateVerifyTimeout
	oldNoContext := delegateNoContext
	cfgFile = cfgPath
	delegateAgent = "worker"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "worktree"
	delegateVerify = nil
	delegateVerifyStrict = []string{"false"}
	delegateVerifyTimeout = 10
	delegateNoContext = true
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		delegateVerify = oldVerify
		delegateVerifyStrict = oldVerifyStrict
		delegateVerifyTimeout = oldVerifyTimeout
		delegateNoContext = oldNoContext
	}()

	var gotErr error
	out := captureStdout(t, func() {
		gotErr = delegateCmd.RunE(delegateCmd, []string{"modify"})
	})
	_ = gotErr

	var got DelegationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\nout=%q", err, out)
	}
	if got.Status != StatusFailed {
		t.Fatalf("expected failed, got %q", got.Status)
	}

	// Main tree should NOT have been modified
	data, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("read file.txt: %v", err)
	}
	if string(data) != "original\n" {
		t.Fatalf("expected main tree unchanged, got %q", string(data))
	}
}

func TestDelegateVerifyRecordFailWorktreeStillApplies(t *testing.T) {
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.name", "Test")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	writeTestFile(t, filepath.Join(dir, "file.txt"), "original\n")
	runGitCmd(t, dir, "add", "file.txt")
	runGitCmd(t, dir, "commit", "-m", "init")

	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  worker:
    name: Worker
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - printf "modified\n" >> file.txt
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldVerify := delegateVerify
	oldVerifyStrict := delegateVerifyStrict
	oldVerifyTimeout := delegateVerifyTimeout
	oldNoContext := delegateNoContext
	cfgFile = cfgPath
	delegateAgent = "worker"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "worktree"
	delegateVerify = []string{"false"} // record mode, not strict
	delegateVerifyStrict = nil
	delegateVerifyTimeout = 10
	delegateNoContext = true
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		delegateVerify = oldVerify
		delegateVerifyStrict = oldVerifyStrict
		delegateVerifyTimeout = oldVerifyTimeout
		delegateNoContext = oldNoContext
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"modify"}); err != nil {
			t.Fatalf("error: %v", err)
		}
	})

	var got DelegationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\nout=%q", err, out)
	}
	if got.Status != StatusCompletedWithVerifyFailures {
		t.Fatalf("expected completed_with_verification_failures, got %q", got.Status)
	}

	// Main tree SHOULD have been modified (record mode still applies)
	data, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("read file.txt: %v", err)
	}
	if string(data) != "original\nmodified\n" {
		t.Fatalf("expected patch applied in record mode, got %q", string(data))
	}
}

func TestDelegateVerifySkippedOnAgentFailure(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  worker:
    name: Worker
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - exit 1
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldVerify := delegateVerify
	oldVerifyStrict := delegateVerifyStrict
	oldVerifyTimeout := delegateVerifyTimeout
	oldNoContext := delegateNoContext
	oldNoFallback := delegateNoFallback
	cfgFile = cfgPath
	delegateAgent = "worker"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "inplace"
	delegateVerify = nil
	delegateVerifyStrict = []string{"echo should-not-run"}
	delegateVerifyTimeout = 10
	delegateNoContext = true
	delegateNoFallback = true
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		delegateVerify = oldVerify
		delegateVerifyStrict = oldVerifyStrict
		delegateVerifyTimeout = oldVerifyTimeout
		delegateNoContext = oldNoContext
		delegateNoFallback = oldNoFallback
	}()

	out := captureStdout(t, func() {
		_ = delegateCmd.RunE(delegateCmd, []string{"task"})
	})

	var got DelegationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\nout=%q", err, out)
	}
	if got.Status != StatusFailed {
		t.Fatalf("expected failed, got %q", got.Status)
	}
	// Verify should not have run because agent failed
	if got.Verify != nil {
		t.Fatalf("expected no verify result when agent fails, got %+v", got.Verify)
	}
}
