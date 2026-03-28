package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestPeriodStartMonthly(t *testing.T) {
	aq := &AgentQuota{ResetMode: "monthly", ResetDay: 1}
	start := aq.PeriodStart()
	now := time.Now()
	if start.Day() != 1 {
		t.Fatalf("expected day=1, got %d", start.Day())
	}
	if start.After(now) {
		t.Fatal("period start should not be in the future")
	}
}

func TestPeriodStartWeekly(t *testing.T) {
	aq := &AgentQuota{ResetMode: "weekly", ResetDay: 1} // Monday
	start := aq.PeriodStart()
	now := time.Now()
	if start.After(now) {
		t.Fatal("period start should not be in the future")
	}
	daysSince := int(now.Sub(start).Hours() / 24)
	if daysSince > 6 {
		t.Fatalf("weekly period should be within 7 days, got %d days ago", daysSince)
	}
}

func TestPeriodStartRollingHours(t *testing.T) {
	aq := &AgentQuota{ResetMode: "rolling_hours", RollingHours: 5}
	start := aq.PeriodStart()
	now := time.Now()
	diff := now.Sub(start)
	if diff < 4*time.Hour+59*time.Minute || diff > 5*time.Hour+1*time.Minute {
		t.Fatalf("expected ~5h rolling window, got %v", diff)
	}
}

func TestPeriodStartRollingHoursDefault(t *testing.T) {
	aq := &AgentQuota{ResetMode: "rolling_hours", RollingHours: 0}
	start := aq.PeriodStart()
	now := time.Now()
	diff := now.Sub(start)
	if diff < 4*time.Hour+59*time.Minute || diff > 5*time.Hour+1*time.Minute {
		t.Fatalf("expected default ~5h rolling window, got %v", diff)
	}
}

func TestEffectiveUnitDefaults(t *testing.T) {
	aq := &AgentQuota{}
	if aq.effectiveUnit() != "calls" {
		t.Fatalf("expected default unit=calls, got %q", aq.effectiveUnit())
	}
	aq.Unit = "hours"
	if aq.effectiveUnit() != "hours" {
		t.Fatalf("expected unit=hours, got %q", aq.effectiveUnit())
	}
}

func TestEffectiveResetModeDefaults(t *testing.T) {
	aq := &AgentQuota{}
	if aq.effectiveResetMode() != "monthly" {
		t.Fatalf("expected default reset_mode=monthly, got %q", aq.effectiveResetMode())
	}
	aq.ResetMode = "weekly"
	if aq.effectiveResetMode() != "weekly" {
		t.Fatalf("expected reset_mode=weekly, got %q", aq.effectiveResetMode())
	}
}

func TestLoadQuotaFileNotFound(t *testing.T) {
	oldHome := os.Getenv("HOME")
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", oldHome)

	qc, err := LoadQuota()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if qc == nil || qc.Agents == nil {
		t.Fatal("expected non-nil QuotaConfig with initialized Agents map")
	}
	if len(qc.Agents) != 0 {
		t.Fatalf("expected empty agents, got %d", len(qc.Agents))
	}
}

func TestSaveAndLoadQuotaRoundtrip(t *testing.T) {
	oldHome := os.Getenv("HOME")
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", oldHome)

	qc := &QuotaConfig{
		Agents: map[string]AgentQuota{
			"claude": {
				Unit:      "calls",
				Limit:     100,
				ResetMode: "monthly",
				ResetDay:  1,
				Notes:     "test quota",
			},
		},
	}

	if err := SaveQuota(qc); err != nil {
		t.Fatalf("SaveQuota: %v", err)
	}

	loaded, err := LoadQuota()
	if err != nil {
		t.Fatalf("LoadQuota: %v", err)
	}

	if len(loaded.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(loaded.Agents))
	}
	claude := loaded.Agents["claude"]
	if claude.Limit != 100 {
		t.Fatalf("expected limit=100, got %d", claude.Limit)
	}
	if claude.Notes != "test quota" {
		t.Fatalf("expected notes=%q, got %q", "test quota", claude.Notes)
	}
}

func TestLoadQuotaInvalidJSON(t *testing.T) {
	oldHome := os.Getenv("HOME")
	home := t.TempDir()
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", oldHome)

	dir := filepath.Join(home, ".config", "quancode")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "quota.json"), []byte("{invalid json"), 0644)

	_, err := LoadQuota()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
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
		ApprovalEvents: []ApprovalEvent{
			{RequestID: "r1", Action: "write", Description: "write file", Decision: "approved"},
		},
		Isolation: "inplace",
		WorkDir:   "/tmp/repo",
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
	if len(decoded.ApprovalEvents) != 1 || decoded.ApprovalEvents[0].RequestID != "r1" {
		t.Fatalf("approval events roundtrip failed: %+v", decoded.ApprovalEvents)
	}
}
