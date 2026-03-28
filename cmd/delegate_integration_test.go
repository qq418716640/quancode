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
	"github.com/qq418716640/quancode/approval"
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
	cfgFile = cfgPath
	delegateAgent = "codex"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "inplace"
	approvalPollInterval = 10 * time.Millisecond
	approvalTimeout = 2 * time.Second
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		approvalPollInterval = oldPoll
		approvalTimeout = oldTimeout
	}()

	go func() {
		time.Sleep(100 * time.Millisecond)
		files, _ := filepath.Glob(filepath.Join(os.TempDir(), "quancode-approval-del_*", "request-req_deadbeef.json"))
		if len(files) != 1 {
			return
		}
		approvalDir := filepath.Dir(files[0])
		_ = approval.WriteResponse(approvalDir, approval.Response{
			RequestID: "req_deadbeef",
			Decision:  "approved",
			DecidedBy: "user",
			Reason:    "confirmed",
		})
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
	if got.Output != "approved-after-response" {
		t.Fatalf("unexpected output: %q", got.Output)
	}
	if len(got.ApprovalEvents) != 1 {
		t.Fatalf("expected 1 approval event, got %#v", got.ApprovalEvents)
	}
	if got.ApprovalEvents[0].RequestID != "req_deadbeef" || got.ApprovalEvents[0].Decision != "approved" {
		t.Fatalf("unexpected approval event: %#v", got.ApprovalEvents[0])
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

	oldCfgFile := cfgFile
	oldAgent := delegateAgent
	oldWorkdir := delegateWorkdir
	oldFormat := delegateFormat
	oldIsolation := delegateIsolation
	oldPoll := approvalPollInterval
	oldTimeout := approvalTimeout
	cfgFile = cfgPath
	delegateAgent = "codex"
	delegateWorkdir = dir
	delegateFormat = "json"
	delegateIsolation = "inplace"
	approvalPollInterval = 10 * time.Millisecond
	approvalTimeout = 50 * time.Millisecond
	defer func() {
		cfgFile = oldCfgFile
		delegateAgent = oldAgent
		delegateWorkdir = oldWorkdir
		delegateFormat = oldFormat
		delegateIsolation = oldIsolation
		approvalPollInterval = oldPoll
		approvalTimeout = oldTimeout
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
