package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/config"
	"github.com/qq418716640/quancode/ledger"
)

func TestResolveReviewSetAgentsFromCSV(t *testing.T) {
	cfg := &config.Config{}
	got, err := resolveReviewSetAgents(cfg, " codex,qoder ,, gemini ,codex ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"codex", "qoder", "gemini"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i, k := range want {
		if got[i] != k {
			t.Errorf("got[%d]=%q, want %q", i, got[i], k)
		}
	}
}

func TestResolveReviewSetAgentsDefaultsToEnabledNonPrimary(t *testing.T) {
	cfg := &config.Config{
		DefaultPrimary: "claude",
		Agents: map[string]config.AgentConfig{
			"claude":  {Enabled: true},
			"codex":   {Enabled: true},
			"qoder":   {Enabled: true},
			"copilot": {Enabled: false},
		},
	}
	got, err := resolveReviewSetAgents(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "codex" || got[1] != "qoder" {
		t.Errorf("expected [codex qoder], got %v", got)
	}
}

func TestResolveReviewSetAgentsRejectsAllBlankCSV(t *testing.T) {
	if _, err := resolveReviewSetAgents(&config.Config{}, " , , "); err == nil {
		t.Fatal("expected error for all-blank csv")
	}
}

func TestHeadLinesTruncates(t *testing.T) {
	s := "a\nb\nc\nd\ne\n"
	got := headLines(s, 2)
	if !strings.HasPrefix(got, "a\nb") {
		t.Errorf("expected prefix a\\nb, got %q", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected truncation marker, got %q", got)
	}
}

func TestHeadLinesNoTruncationWhenShort(t *testing.T) {
	s := "a\nb\n"
	if headLines(s, 50) != s {
		t.Errorf("short input should pass through unchanged")
	}
}

func TestHeadLinesZeroReturnsAll(t *testing.T) {
	s := "a\nb\nc\n"
	if headLines(s, 0) != s {
		t.Errorf("n<=0 should return whole string")
	}
}

// TestReviewSetParallelFanoutAndGroupID verifies that two fake agents
// run in parallel, each gets its own DelegationID and RunID, and both
// ledger entries share the same ReviewSetID.
func TestReviewSetParallelFanoutAndGroupID(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude
    command: /bin/sh
    enabled: true
  codex:
    name: Codex
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - printf codex-output
  qoder:
    name: Qoder
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - printf qoder-output
`)
	withReviewSetFlags(t, cfgPath, dir, func() {
		reviewSetAgents = "codex,qoder"
		reviewSetFormat = "json"
		out := captureStdout(t, func() {
			if err := runReviewSet(reviewSetCmd, []string{"compare", "this"}); err != nil {
				t.Fatalf("runReviewSet error: %v", err)
			}
		})
		var rs ReviewSetResult
		if err := json.Unmarshal([]byte(out), &rs); err != nil {
			t.Fatalf("unmarshal: %v\nout=%q", err, out)
		}
		if !strings.HasPrefix(rs.ReviewSetID, "rs_") {
			t.Errorf("expected rs_ prefix, got %q", rs.ReviewSetID)
		}
		if len(rs.Results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(rs.Results))
		}
		if rs.Failed != 0 {
			t.Errorf("expected 0 failed, got %d", rs.Failed)
		}
		gotAgents := map[string]string{}
		for _, r := range rs.Results {
			gotAgents[r.Agent] = r.Output
			if r.DelegationID == "" {
				t.Errorf("agent %s missing delegation_id", r.Agent)
			}
		}
		if gotAgents["codex"] != "codex-output" {
			t.Errorf("codex output = %q", gotAgents["codex"])
		}
		if gotAgents["qoder"] != "qoder-output" {
			t.Errorf("qoder output = %q", gotAgents["qoder"])
		}

		// Verify all ledger entries from this test share the same review_set_id.
		entries, err := ledger.ReadAll()
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		var matched []ledger.Entry
		for _, e := range entries {
			if e.ReviewSetID == rs.ReviewSetID {
				matched = append(matched, e)
			}
		}
		if len(matched) != 2 {
			t.Fatalf("expected 2 ledger entries with review_set_id %q, got %d", rs.ReviewSetID, len(matched))
		}
		seenAgents := map[string]bool{}
		seenRunIDs := map[string]bool{}
		for _, e := range matched {
			seenAgents[e.Agent] = true
			if e.RunID == "" {
				t.Errorf("ledger entry %s missing run_id", e.DelegationID)
			}
			if seenRunIDs[e.RunID] {
				t.Errorf("run_id %q shared across siblings — should be unique per agent", e.RunID)
			}
			seenRunIDs[e.RunID] = true
		}
		if !seenAgents["codex"] || !seenAgents["qoder"] {
			t.Errorf("expected both agents in ledger, got %v", seenAgents)
		}
	})
}

// TestReviewSetExitCodeOnPartialFailure ensures a non-zero exit when any
// sibling fails, while still letting the others complete.
func TestReviewSetExitCodeOnPartialFailure(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    name: Claude
    command: /bin/sh
    enabled: true
  codex:
    name: Codex
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - printf good
  qoder:
    name: Qoder
    command: /bin/sh
    enabled: true
    delegate_args:
      - -c
      - exit 7
`)
	withReviewSetFlags(t, cfgPath, dir, func() {
		reviewSetAgents = "codex,qoder"
		reviewSetFormat = "json"
		var runErr error
		out := captureStdout(t, func() {
			runErr = runReviewSet(reviewSetCmd, []string{"mixed", "outcome"})
		})
		var ese *agent.ExitStatusError
		if !errors.As(runErr, &ese) || ese.Code != 1 {
			t.Fatalf("expected ExitStatusError{Code:1}, got %v", runErr)
		}
		var rs ReviewSetResult
		if err := json.Unmarshal([]byte(out), &rs); err != nil {
			t.Fatalf("unmarshal: %v\nout=%q", err, out)
		}
		if rs.Failed != 1 {
			t.Errorf("expected failed=1, got %d", rs.Failed)
		}
		// codex success kept even though qoder failed
		var codexOK, qoderFail bool
		for _, r := range rs.Results {
			if r.Agent == "codex" && r.Status == "completed" {
				codexOK = true
			}
			if r.Agent == "qoder" && r.Status != "completed" {
				qoderFail = true
			}
		}
		if !codexOK || !qoderFail {
			t.Errorf("expected codex completed and qoder failed, got %+v", rs.Results)
		}
	})
}

// TestReviewSetUsesCustomGroupID confirms a user-supplied --group-id is
// passed straight into ledger entries instead of being overwritten with
// an auto-generated rs_<hex>.
func TestReviewSetUsesCustomGroupID(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    command: /bin/sh
    enabled: true
  codex:
    command: /bin/sh
    enabled: true
    delegate_args: ["-c", "printf hello"]
  qoder:
    command: /bin/sh
    enabled: true
    delegate_args: ["-c", "printf world"]
`)
	withReviewSetFlags(t, cfgPath, dir, func() {
		reviewSetAgents = "codex,qoder"
		reviewSetGroupID = "rs_my_custom_id"
		reviewSetFormat = "json"
		out := captureStdout(t, func() {
			if err := runReviewSet(reviewSetCmd, []string{"x"}); err != nil {
				t.Fatalf("runE: %v", err)
			}
		})
		var rs ReviewSetResult
		if err := json.Unmarshal([]byte(out), &rs); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if rs.ReviewSetID != "rs_my_custom_id" {
			t.Errorf("expected custom group id preserved, got %q", rs.ReviewSetID)
		}
		entries, _ := ledger.ReadAll()
		var hits int
		for _, e := range entries {
			if e.ReviewSetID == "rs_my_custom_id" {
				hits++
			}
		}
		if hits != 2 {
			t.Errorf("expected 2 ledger entries with custom id, got %d", hits)
		}
	})
}

// TestReviewSetJSONFullSchema asserts every advertised field is present
// in the JSON output (catches accidental field drops on future refactors).
func TestReviewSetJSONFullSchema(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    command: /bin/sh
    enabled: true
  codex:
    command: /bin/sh
    enabled: true
    delegate_args: ["-c", "printf a"]
  qoder:
    command: /bin/sh
    enabled: true
    delegate_args: ["-c", "printf b"]
`)
	withReviewSetFlags(t, cfgPath, dir, func() {
		reviewSetAgents = "codex,qoder"
		reviewSetFormat = "json"
		out := captureStdout(t, func() {
			_ = runReviewSet(reviewSetCmd, []string{"task"})
		})
		var raw map[string]any
		if err := json.Unmarshal([]byte(out), &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		for _, key := range []string{"review_set_id", "task", "agents", "isolation", "work_dir", "duration_ms", "results", "failed"} {
			if _, ok := raw[key]; !ok {
				t.Errorf("missing required key %q in JSON output: %v", key, raw)
			}
		}
	})
}

// TestReviewSetRejectsInvalidFormat enforces enum validation on --format
// (regression: earlier versions silently fell back to text on garbage).
func TestReviewSetRejectsInvalidFormat(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    command: /bin/sh
    enabled: true
  codex:
    command: /bin/sh
    enabled: true
  qoder:
    command: /bin/sh
    enabled: true
`)
	withReviewSetFlags(t, cfgPath, dir, func() {
		reviewSetAgents = "codex,qoder"
		reviewSetFormat = "yaml"
		err := runReviewSet(reviewSetCmd, []string{"x"})
		if err == nil || !strings.Contains(err.Error(), "--format") {
			t.Fatalf("expected --format validation error, got %v", err)
		}
	})
}

// TestReviewSetRequiresDefaultPrimaryWhenNoAgentsPassed verifies the empty
// CSV path needs default_primary so we never silently fan-out to ourselves.
func TestReviewSetRequiresDefaultPrimaryWhenNoAgentsPassed(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"codex": {Enabled: true},
			"qoder": {Enabled: true},
		},
	}
	if _, err := resolveReviewSetAgents(cfg, ""); err == nil {
		t.Fatal("expected error when default_primary is empty and no --agents passed")
	}
}

// TestReviewSetRequiresAtLeastTwoAgents enforces the minimum agent count.
func TestReviewSetRequiresAtLeastTwoAgents(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
default_primary: claude
agents:
  claude:
    command: /bin/sh
    enabled: true
  codex:
    command: /bin/sh
    enabled: true
`)
	withReviewSetFlags(t, cfgPath, dir, func() {
		reviewSetAgents = "codex"
		err := runReviewSet(reviewSetCmd, []string{"x"})
		if err == nil || !strings.Contains(err.Error(), "at least 2 agents") {
			t.Fatalf("expected 'at least 2 agents' error, got %v", err)
		}
	})
}

// withReviewSetFlags resets review-set globals after the test body runs so
// parallel tests don't bleed flag state.
func withReviewSetFlags(t *testing.T, cfgPath, workDir string, fn func()) {
	t.Helper()
	oldCfg := cfgFile
	oldAgents := reviewSetAgents
	oldGroup := reviewSetGroupID
	oldWorkdir := reviewSetWorkdir
	oldFormat := reviewSetFormat
	oldIso := reviewSetIsolation
	oldNoCtx := reviewSetNoContext
	oldDiff := reviewSetContextDiff
	oldHead := reviewSetHeadLines
	cfgFile = cfgPath
	reviewSetWorkdir = workDir
	reviewSetNoContext = true // avoid pulling project files into fake-agent invocation
	reviewSetHeadLines = 50
	defer func() {
		cfgFile = oldCfg
		reviewSetAgents = oldAgents
		reviewSetGroupID = oldGroup
		reviewSetWorkdir = oldWorkdir
		reviewSetFormat = oldFormat
		reviewSetIsolation = oldIso
		reviewSetNoContext = oldNoCtx
		reviewSetContextDiff = oldDiff
		reviewSetHeadLines = oldHead
		_ = os.Unsetenv("QUANCODE_TEST")
	}()
	fn()
}
