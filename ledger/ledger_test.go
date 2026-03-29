package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAndReadAll(t *testing.T) {
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
	if entries[0].Agent != "codex" || entries[0].Task != "push" {
		t.Fatalf("unexpected entry: %+v", entries[0])
	}

	logs, err := filepath.Glob(filepath.Join(home, ".config", "quancode", "logs", "*.jsonl"))
	if err != nil {
		t.Fatalf("Glob logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected one log file, got %v", logs)
	}
}

func TestAppendAutoFillsTimestamp(t *testing.T) {
	oldHome := os.Getenv("HOME")
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", oldHome)

	entry := &Entry{
		Agent:   "claude",
		Task:    "test",
		WorkDir: "/tmp",
	}
	if err := Append(entry); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if entry.Timestamp == "" {
		t.Fatal("expected timestamp to be auto-filled")
	}
	if _, err := time.Parse(time.RFC3339, entry.Timestamp); err != nil {
		t.Fatalf("expected valid RFC3339 timestamp, got %q: %v", entry.Timestamp, err)
	}
}

func TestReadSinceFiltersOldEntries(t *testing.T) {
	oldHome := os.Getenv("HOME")
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", oldHome)

	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	for _, ts := range []time.Time{old, recent} {
		entry := &Entry{
			Timestamp: ts.Format(time.RFC3339),
			Agent:     "claude",
			Task:      "task-" + ts.Format("15:04"),
			WorkDir:   "/tmp",
		}
		if err := Append(entry); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	since := now.Add(-2 * time.Hour)
	entries, err := ReadSince(since)
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after filtering, got %d", len(entries))
	}
	if entries[0].Timestamp != recent.Format(time.RFC3339) {
		t.Fatalf("expected recent entry, got %q", entries[0].Timestamp)
	}
}

func TestSplitNonEmpty(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single line no newline", "hello", []string{"hello"}},
		{"single line with newline", "hello\n", []string{"hello"}},
		{"multiple lines", "a\nb\nc\n", []string{"a", "b", "c"}},
		{"blank lines", "a\n\nb\n\n", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitNonEmpty([]byte(tt.in))
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len mismatch: got %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("index %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestEntryJSONRoundtrip(t *testing.T) {
	entry := Entry{
		Timestamp:    "2026-03-28T00:00:00Z",
		Agent:        "claude",
		Task:         "test task",
		ExitCode:     0,
		DurationMs:   1500,
		ChangedFiles: []string{"main.go"},
		Isolation:    "inplace",
		WorkDir:      "/tmp/repo",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Entry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Agent != entry.Agent || decoded.Task != entry.Task {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", entry, decoded)
	}
}

func TestEntryRunTrackingFieldsRoundtrip(t *testing.T) {
	entry := Entry{
		Timestamp:      "2026-03-29T00:00:00Z",
		Agent:          "claude",
		Task:           "fix bug",
		WorkDir:        "/tmp/repo",
		RunID:          "run_abc123",
		Attempt:        2,
		FallbackFrom:   "codex",
		FallbackReason: "timed_out",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Entry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.RunID != "run_abc123" {
		t.Fatalf("expected run_id=run_abc123, got %q", decoded.RunID)
	}
	if decoded.Attempt != 2 {
		t.Fatalf("expected attempt=2, got %d", decoded.Attempt)
	}
	if decoded.FallbackFrom != "codex" {
		t.Fatalf("expected fallback_from=codex, got %q", decoded.FallbackFrom)
	}
	if decoded.FallbackReason != "timed_out" {
		t.Fatalf("expected fallback_reason=timed_out, got %q", decoded.FallbackReason)
	}

	// Verify JSON key names are correctly spelled
	jsonStr := string(data)
	for _, key := range []string{`"run_id"`, `"attempt"`, `"fallback_from"`, `"fallback_reason"`} {
		if !strings.Contains(jsonStr, key) {
			t.Fatalf("expected JSON key %s in output, got: %s", key, jsonStr)
		}
	}
}

func TestEntryRunTrackingOmittedWhenEmpty(t *testing.T) {
	entry := Entry{
		Timestamp: "2026-03-29T00:00:00Z",
		Agent:     "codex",
		Task:      "write tests",
		WorkDir:   "/tmp/repo",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	jsonStr := string(data)
	for _, field := range []string{"run_id", "attempt", "fallback_from", "fallback_reason"} {
		if strings.Contains(jsonStr, field) {
			t.Fatalf("expected %q to be omitted from JSON, got: %s", field, jsonStr)
		}
	}
}

func TestOldEntriesWithoutRunTrackingFieldsUnmarshalSafely(t *testing.T) {
	// Simulate a legacy JSONL entry without run tracking fields
	oldJSON := `{"timestamp":"2026-03-01T00:00:00Z","agent":"codex","task":"old task","exit_code":0,"timed_out":false,"duration_ms":100,"work_dir":"/tmp"}`

	var entry Entry
	if err := json.Unmarshal([]byte(oldJSON), &entry); err != nil {
		t.Fatalf("Unmarshal old entry: %v", err)
	}

	if entry.RunID != "" || entry.Attempt != 0 || entry.FallbackFrom != "" || entry.FallbackReason != "" {
		t.Fatalf("expected zero values for new fields on old data, got: RunID=%q Attempt=%d FallbackFrom=%q FallbackReason=%q",
			entry.RunID, entry.Attempt, entry.FallbackFrom, entry.FallbackReason)
	}
}
