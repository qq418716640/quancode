package ledger

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendAndReadAllPreservesApprovalEvents(t *testing.T) {
	oldHome := os.Getenv("HOME")
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("Setenv HOME: %v", err)
	}
	defer os.Setenv("HOME", oldHome)

	entry := &Entry{
		Agent:      "codex",
		Task:       "push",
		WorkDir:    "/tmp/repo",
		ExitCode:   0,
		DurationMs: 42,
		ApprovalEvents: []ApprovalEvent{
			{
				RequestID:   "req_deadbeef",
				Action:      "git_push_force",
				Description: "Force-push branch",
				Decision:    "approved",
			},
		},
	}
	if err := Append(entry); err != nil {
		t.Fatalf("Append: %v", err)
	}

	entries, err := ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if len(entries[0].ApprovalEvents) != 1 {
		t.Fatalf("expected approval events to be preserved, got %#v", entries[0].ApprovalEvents)
	}
	got := entries[0].ApprovalEvents[0]
	if got.RequestID != "req_deadbeef" || got.Decision != "approved" {
		t.Fatalf("unexpected approval event: %#v", got)
	}

	logs, err := filepath.Glob(filepath.Join(home, ".config", "quancode", "logs", "*.jsonl"))
	if err != nil {
		t.Fatalf("Glob logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected one log file, got %v", logs)
	}
}
