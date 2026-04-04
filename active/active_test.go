package active

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func setupTestDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dirOnce = sync.Once{}
	dirValue = ""
	dirOnce.Do(func() { dirValue = dir })
}

func TestRegisterUnregister(t *testing.T) {
	setupTestDir(t)

	Register("del_test1", "codex", "test task", "/tmp/work")

	files, _ := os.ReadDir(Dir())
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Name() != "del_test1.json" {
		t.Errorf("unexpected file: %s", files[0].Name())
	}

	Unregister("del_test1")

	files, _ = os.ReadDir(Dir())
	if len(files) != 0 {
		t.Fatalf("expected 0 files after unregister, got %d", len(files))
	}
}

func TestListReturnsAliveEntries(t *testing.T) {
	setupTestDir(t)

	// Register with current process PID (which is alive)
	Register("del_alive", "codex", "alive task", "/tmp")

	entries := List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].DelegationID != "del_alive" {
		t.Errorf("expected del_alive, got %s", entries[0].DelegationID)
	}

	Unregister("del_alive")
}

func TestListCleansStaleEntries(t *testing.T) {
	setupTestDir(t)

	// Write a fake entry with a dead PID
	path := filepath.Join(Dir(), "del_dead.json")
	data := `{"delegation_id":"del_dead","agent":"x","pid":999999,"pid_start_time":1,"started_at":"2026-01-01T00:00:00Z"}`
	os.WriteFile(path, []byte(data), 0644)

	entries := List()
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries (stale cleaned), got %d", len(entries))
	}

	// File should have been removed
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("stale file was not cleaned up")
	}
}

func TestListEmptyDir(t *testing.T) {
	setupTestDir(t)
	entries := List()
	if entries != nil {
		t.Errorf("expected nil, got %v", entries)
	}
}

func TestRegisterSilentOnBadDir(t *testing.T) {
	dirOnce = sync.Once{}
	dirValue = ""
	dirOnce.Do(func() { dirValue = "/nonexistent/path/active" })

	// Should not panic
	Register("del_x", "codex", "task", "/tmp")
}
