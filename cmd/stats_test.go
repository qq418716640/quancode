package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/qq418716640/quancode/ledger"
)

func TestStatsIncludesApprovalSummary(t *testing.T) {
	oldHome := os.Getenv("HOME")
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("Setenv HOME: %v", err)
	}
	defer os.Setenv("HOME", oldHome)

	if err := ledger.Append(&ledger.Entry{
		Agent:      "codex",
		Task:       "push",
		WorkDir:    "/tmp/repo",
		ExitCode:   0,
		DurationMs: 42,
		ApprovalEvents: []ledger.ApprovalEvent{
			{RequestID: "req_1", Action: "git_push_force", Description: "Force-push branch", Decision: "approved"},
			{RequestID: "req_2", Action: "delete_file", Description: "Delete file", Decision: "denied"},
		},
	}); err != nil {
		t.Fatalf("Append entry 1: %v", err)
	}
	if err := ledger.Append(&ledger.Entry{
		Agent:      "claude",
		Task:       "edit",
		WorkDir:    "/tmp/repo",
		ExitCode:   0,
		DurationMs: 24,
		ApprovalEvents: []ledger.ApprovalEvent{
			{RequestID: "req_3", Action: "update_ci", Description: "Update CI", Decision: "approved"},
		},
	}); err != nil {
		t.Fatalf("Append entry 2: %v", err)
	}

	oldDays := statsDays
	statsDays = 30
	defer func() { statsDays = oldDays }()

	out := captureStdout(t, func() {
		if err := statsCmd.RunE(statsCmd, nil); err != nil {
			t.Fatalf("stats RunE: %v", err)
		}
	})

	for _, want := range []string{
		"approval summary",
		"3 requests",
		"2 approved",
		"1 denied",
		"last delegation with approval at",
		"APPROVALS",
		"codex",
		"claude",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected stats output to contain %q, got:\n%s", want, out)
		}
	}

	for _, pattern := range []string{
		`(?m)^claude\s+1\s+100%\s+0\s+0\s+24ms\s+24ms\s+0\s+1$`,
		`(?m)^codex\s+1\s+100%\s+0\s+0\s+42ms\s+42ms\s+0\s+2$`,
	} {
		if !regexp.MustCompile(pattern).MatchString(out) {
			t.Fatalf("expected stats output to match %q, got:\n%s", pattern, out)
		}
	}
}
