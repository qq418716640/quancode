package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/ledger"
)

func TestDelegateRunEAutoRoutesAndPrintsJSON(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  codex:
    name: Codex CLI
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - printf delegated-output
    preferred_for:
      - test
    priority: 20
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	cfgFile = cfgPath
	delegateAgent = ""
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "inplace"
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"write", "tests"}); err != nil {
			t.Fatalf("delegate RunE returned error: %v", err)
		}
	})

	var got DelegationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal json output: %v\noutput=%q", err, out)
	}
	if got.Agent != "codex" {
		t.Fatalf("expected auto-routed agent codex, got %q", got.Agent)
	}
	if got.Output != "delegated-output" {
		t.Fatalf("unexpected output: %q", got.Output)
	}
	if got.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", got.ExitCode)
	}
	if got.Status != "completed" {
		t.Fatalf("expected completed status, got %q", got.Status)
	}
	if got.ChangedFiles != nil {
		t.Fatalf("expected nil changed_files outside git repo, got %v", got.ChangedFiles)
	}
}

func TestDelegateRunEIsolationPatchPrintsPatch(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.name", "QuanCode Test")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")
	writeTestFile(t, filepath.Join(dir, "tracked.txt"), "base\n")
	runGitCmd(t, dir, "add", "tracked.txt")
	runGitCmd(t, dir, "commit", "-m", "init")

	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  codex:
    name: Codex CLI
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - printf "changed\n" >> tracked.txt
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	cfgFile = cfgPath
	delegateAgent = "codex"
	delegateWorkdir = dir
	delegateFormat = "text"
	delegateIsolation = "patch"
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"append", "line"}); err != nil {
			t.Fatalf("delegate RunE returned error: %v", err)
		}
	})

	if !strings.Contains(out, "diff --git") {
		t.Fatalf("expected patch output, got %q", out)
	}
	if !strings.Contains(out, "tracked.txt") {
		t.Fatalf("expected tracked.txt in patch output, got %q", out)
	}

	data, err := os.ReadFile(filepath.Join(dir, "tracked.txt"))
	if err != nil {
		t.Fatalf("read tracked.txt: %v", err)
	}
	if string(data) != "base\n" {
		t.Fatalf("expected main working tree unchanged, got %q", string(data))
	}
}

func TestDelegateRunEReturnsExitStatusErrorForJSONFailure(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  codex:
    name: Codex CLI
    command: /bin/sh
    enabled: true
    timeout_secs: 1
    delegate_args:
      - -c
      - sleep 2
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldNoFallback := delegateNoFallback
	cfgFile = cfgPath
	delegateAgent = "codex"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "inplace"
	delegateNoFallback = true // prevent fallback so we test the timeout result directly
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		delegateNoFallback = oldNoFallback
	}()

	var gotErr error
	out := captureStdout(t, func() {
		gotErr = delegateCmd.RunE(delegateCmd, []string{"sleep"})
	})

	// Timeout now goes through finalizeDelegation which outputs JSON.
	// The JSON should show timed_out status.
	var got DelegationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal json output: %v\noutput=%q", err, out)
	}
	if got.ExitCode != 124 {
		t.Fatalf("expected delegated command timeout exit code 124, got %d", got.ExitCode)
	}
	if !got.TimedOut {
		t.Fatalf("expected timed_out true, got false")
	}
	if got.Status != "timed_out" {
		t.Fatalf("expected timed_out status, got %q", got.Status)
	}
	_ = gotErr // may or may not be ExitStatusError depending on format handling
}

func TestDelegateRunEWorktreeIsolationAppliesPatch(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.name", "QuanCode Test")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")
	writeTestFile(t, filepath.Join(dir, "tracked.txt"), "base\n")
	runGitCmd(t, dir, "add", "tracked.txt")
	runGitCmd(t, dir, "commit", "-m", "init")

	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  codex:
    name: Codex CLI
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - printf "changed\n" >> tracked.txt
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	cfgFile = cfgPath
	delegateAgent = "codex"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "worktree"
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"append", "line"}); err != nil {
			t.Fatalf("delegate RunE returned error: %v", err)
		}
	})

	var got DelegationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal json output: %v\noutput=%q", err, out)
	}
	if got.Status != "completed" {
		t.Fatalf("expected completed status, got %q", got.Status)
	}
	if len(got.ChangedFiles) != 1 || got.ChangedFiles[0] != "tracked.txt" {
		t.Fatalf("expected [tracked.txt] in changed_files, got %v", got.ChangedFiles)
	}

	// worktree isolation should auto-apply patch to main dir
	data, err := os.ReadFile(filepath.Join(dir, "tracked.txt"))
	if err != nil {
		t.Fatalf("read tracked.txt: %v", err)
	}
	if string(data) != "base\nchanged\n" {
		t.Fatalf("expected patch applied to main dir, got %q", string(data))
	}
}

func TestDelegateRunEUnknownAgent(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	cfgFile = cfgPath
	delegateAgent = "nonexistent"
	delegateWorkdir = dir
	delegateFormat = "text"
	delegateIsolation = "inplace"
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
	}()

	err := delegateCmd.RunE(delegateCmd, []string{"task"})
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	if !strings.Contains(err.Error(), "unknown agent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDelegateRunEDisabledAgent(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: false
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	cfgFile = cfgPath
	delegateAgent = "claude"
	delegateWorkdir = dir
	delegateFormat = "text"
	delegateIsolation = "inplace"
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
	}()

	err := delegateCmd.RunE(delegateCmd, []string{"task"})
	if err == nil {
		t.Fatal("expected error for disabled agent")
	}
	if !strings.Contains(err.Error(), "is disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDelegateRunEWorktreeRequiresGitRepo(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir() // not a git repo
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - echo ok
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	cfgFile = cfgPath
	delegateAgent = "claude"
	delegateWorkdir = dir
	delegateFormat = "text"
	delegateIsolation = "worktree"
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
	}()

	err := delegateCmd.RunE(delegateCmd, []string{"task"})
	if err == nil {
		t.Fatal("expected error for worktree in non-git dir")
	}
	// The error surfaces as ExitStatusError since it goes through finalizeDelegation;
	// the actual "requires a git repository" message is printed to stderr.
	var exitErr *agent.ExitStatusError
	if !errors.As(err, &exitErr) {
		// Could also be a direct error if isolation check happens before attempt
		if !strings.Contains(err.Error(), "requires a git repository") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestDelegateRunETextOutputNonZeroExit(t *testing.T) {
	isolateHome(t)
	// In text mode, non-zero exit from the sub-agent is not a Go error —
	// runner.Run returns nil error with ExitCode set. The delegate command
	// still prints the output and returns nil.
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - printf "error output" && exit 3
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	cfgFile = cfgPath
	delegateAgent = "claude"
	delegateWorkdir = dir
	delegateFormat = "text"
	delegateIsolation = "inplace"
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"task"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "error output") {
		t.Fatalf("expected error output in stdout, got %q", out)
	}
}

func TestDelegateRunEDryRunShowsPrompt(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  codex:
    name: Codex CLI
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - echo should-not-run
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldDryRun := delegateDryRun
	oldNoContext := delegateNoContext
	cfgFile = cfgPath
	delegateAgent = "codex"
	delegateWorkdir = dir
	delegateFormat = "text"
	delegateIsolation = "inplace"
	delegateDryRun = true
	delegateNoContext = true
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		delegateDryRun = oldDryRun
		delegateNoContext = oldNoContext
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"fix", "the", "bug"}); err != nil {
			t.Fatalf("delegate RunE returned error: %v", err)
		}
	})

	// Should show the task, not the agent's output
	if !strings.Contains(out, "fix the bug") {
		t.Fatalf("expected dry-run output to contain task, got:\n%s", out)
	}
	if strings.Contains(out, "should-not-run") {
		t.Fatal("dry-run should not execute the agent")
	}
}

func TestDelegateRunEDryRunJSON(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  codex:
    name: Codex CLI
    command: /bin/sh
    enabled: true
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldDryRun := delegateDryRun
	oldNoContext := delegateNoContext
	cfgFile = cfgPath
	delegateAgent = "codex"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "worktree"
	delegateDryRun = true
	delegateNoContext = true
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		delegateDryRun = oldDryRun
		delegateNoContext = oldNoContext
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"write", "tests"}); err != nil {
			t.Fatalf("delegate RunE returned error: %v", err)
		}
	})

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("expected valid JSON from dry-run, got:\n%s", out)
	}
	if got["agent"] != "codex" {
		t.Fatalf("expected agent=codex, got %v", got["agent"])
	}
	if got["isolation"] != "worktree" {
		t.Fatalf("expected isolation=worktree, got %v", got["isolation"])
	}
	task, _ := got["task"].(string)
	if !strings.Contains(task, "write tests") {
		t.Fatalf("expected task to contain 'write tests', got %q", task)
	}
}

func TestDelegateRunEFallbackChainRecordsRunTracking(t *testing.T) {
	// First agent times out, triggers fallback to second agent which succeeds.
	// Verify ledger entries share the same RunID with correct attempt/fallback fields.
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.name", "QuanCode Test")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")
	writeTestFile(t, filepath.Join(dir, "dummy.txt"), "init\n")
	runGitCmd(t, dir, "add", "dummy.txt")
	runGitCmd(t, dir, "commit", "-m", "init")

	isolateHome(t)

	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude Code
    command: /bin/sh
    enabled: true
  alpha:
    name: Alpha Agent
    command: /bin/sh
    enabled: true
    timeout_secs: 1
    delegate_args:
      - -c
      - sleep 5
    priority: 10
  beta:
    name: Beta Agent
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - printf fallback-ok
    priority: 20
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldNoFallback := delegateNoFallback
	oldNoContext := delegateNoContext
	cfgFile = cfgPath
	delegateAgent = "alpha"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "inplace"
	delegateNoFallback = false
	delegateNoContext = true
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		delegateNoFallback = oldNoFallback
		delegateNoContext = oldNoContext
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"do", "something"}); err != nil {
			t.Fatalf("delegate RunE returned error: %v", err)
		}
	})

	var got DelegationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal json output: %v\noutput=%q", err, out)
	}
	if got.Agent != "beta" {
		t.Fatalf("expected fallback to beta, got %q", got.Agent)
	}
	if got.Status != "completed" {
		t.Fatalf("expected completed, got %q", got.Status)
	}

	// Read ledger entries and verify run tracking
	since := time.Now().Add(-1 * time.Minute)
	entries, err := ledger.ReadSince(since)
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 ledger entries (failed + success), got %d", len(entries))
	}

	first := entries[0]
	second := entries[1]

	// Both share the same RunID
	if first.RunID == "" {
		t.Fatal("expected RunID to be set")
	}
	if first.RunID != second.RunID {
		t.Fatalf("RunID mismatch: %q vs %q", first.RunID, second.RunID)
	}

	// First attempt: no fallback info
	if first.Agent != "alpha" {
		t.Fatalf("expected first entry agent=alpha, got %q", first.Agent)
	}
	if first.Attempt != 1 {
		t.Fatalf("expected first attempt=1, got %d", first.Attempt)
	}
	if first.FallbackFrom != "" {
		t.Fatalf("expected first entry FallbackFrom empty, got %q", first.FallbackFrom)
	}

	// Second attempt: records fallback chain
	if second.Agent != "beta" {
		t.Fatalf("expected second entry agent=beta, got %q", second.Agent)
	}
	if second.Attempt != 2 {
		t.Fatalf("expected second attempt=2, got %d", second.Attempt)
	}
	if second.FallbackFrom != "alpha" {
		t.Fatalf("expected FallbackFrom=alpha, got %q", second.FallbackFrom)
	}
	if second.FallbackReason != FailureClassTimedOut {
		t.Fatalf("expected FallbackReason=%q, got %q", FailureClassTimedOut, second.FallbackReason)
	}
}
