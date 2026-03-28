package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qq418716640/quancode/agent"
)

func TestDelegateRunEAutoRoutesAndPrintsJSON(t *testing.T) {
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
	cfgFile = cfgPath
	delegateAgent = "codex"
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

	var gotErr error
	out := captureStdout(t, func() {
		gotErr = delegateCmd.RunE(delegateCmd, []string{"sleep"})
	})

	var exitErr *agent.ExitStatusError
	if !errors.As(gotErr, &exitErr) {
		t.Fatalf("expected ExitStatusError, got %v", gotErr)
	}
	if exitErr.Code != 1 {
		t.Fatalf("expected exit code 1, got %d", exitErr.Code)
	}

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
}

func TestDelegateRunERecordsApprovalEventAndDecision(t *testing.T) {
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
    timeout_secs: 5
    delegate_args:
      - -c
      - |
        req="$QUANCODE_APPROVAL_DIR/request-req_deadbeef.json"
        cat > "$req.tmp" <<EOF
        {
          "schema_version": 1,
          "request_id": "req_deadbeef",
          "delegation_id": "$QUANCODE_DELEGATION_ID",
          "timestamp": "2026-03-27T12:00:00Z",
          "action": "git_push_force",
          "description": "Force-push branch"
        }
        EOF
        mv "$req.tmp" "$req"
        while [ ! -f "$QUANCODE_APPROVAL_DIR/response-req_deadbeef.json" ]; do
          sleep 0.05
        done
        printf approved-after-response
`)

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldPoll := approvalPollInterval
	oldTimeout := approvalTimeout
	oldReader := stdinReader
	cfgFile = cfgPath
	delegateAgent = "codex"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "inplace"
	approvalPollInterval = 10 * time.Millisecond
	approvalTimeout = 2 * time.Second
	// Simulate user typing "y\n" for the interactive approval prompt
	stdinReader = bufio.NewReader(strings.NewReader("y\n"))
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		approvalPollInterval = oldPoll
		approvalTimeout = oldTimeout
		stdinReader = oldReader
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"push"}); err != nil {
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
	if !strings.Contains(got.Output, "approved-after-response") {
		t.Fatalf("expected output to contain approved-after-response, got %q", got.Output)
	}
	if len(got.ApprovalEvents) != 1 {
		t.Fatalf("expected 1 approval event, got %#v", got.ApprovalEvents)
	}
	if got.ApprovalEvents[0].RequestID != "req_deadbeef" || got.ApprovalEvents[0].Decision != "approved" {
		t.Fatalf("unexpected approval event: %#v", got.ApprovalEvents[0])
	}
}

func TestDelegateRunEWorktreeIsolationAppliesPatch(t *testing.T) {
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
	if !strings.Contains(err.Error(), "requires a git repository") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDelegateRunETextOutputNonZeroExit(t *testing.T) {
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

func TestDelegateRunETimeoutDeniesPendingApproval(t *testing.T) {
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
    timeout_secs: 2
    delegate_args:
      - -c
      - |
        req="$QUANCODE_APPROVAL_DIR/request-req_deadbeef.json"
        cat > "$req.tmp" <<EOF
        {
          "schema_version": 1,
          "request_id": "req_deadbeef",
          "delegation_id": "$QUANCODE_DELEGATION_ID",
          "timestamp": "2026-03-27T12:00:00Z",
          "action": "delete_file",
          "description": "Delete file"
        }
        EOF
        mv "$req.tmp" "$req"
        while [ ! -f "$QUANCODE_APPROVAL_DIR/response-req_deadbeef.json" ]; do
          sleep 0.05
        done
        if grep -q 'denied' "$QUANCODE_APPROVAL_DIR/response-req_deadbeef.json"; then
          exit 7
        fi
        exit 9
`)

	// Use a pipe that never writes — simulates user not responding, so timeout fires
	pipeR, pipeW, _ := os.Pipe()
	defer pipeR.Close()
	defer pipeW.Close()

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldPoll := approvalPollInterval
	oldTimeout := approvalTimeout
	oldReader := stdinReader
	cfgFile = cfgPath
	delegateAgent = "codex"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "inplace"
	approvalPollInterval = 10 * time.Millisecond
	approvalTimeout = 50 * time.Millisecond
	stdinReader = bufio.NewReader(pipeR)
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		approvalPollInterval = oldPoll
		approvalTimeout = oldTimeout
		stdinReader = oldReader
	}()

	out := captureStdout(t, func() {
		if err := delegateCmd.RunE(delegateCmd, []string{"delete"}); err != nil {
			t.Fatalf("delegate RunE returned error: %v", err)
		}
	})

	var got DelegationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal json output: %v\noutput=%q", err, out)
	}
	if got.Status != "failed" {
		t.Fatalf("expected failed status, got %q", got.Status)
	}
	if got.ExitCode != 7 {
		t.Fatalf("expected delegated exit code 7 after timeout deny, got %d", got.ExitCode)
	}
	if len(got.ApprovalEvents) != 1 || got.ApprovalEvents[0].Decision != "denied" {
		t.Fatalf("unexpected approval events: %#v", got.ApprovalEvents)
	}
}
