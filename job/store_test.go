package job

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestDir(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	orig := os.Getenv("HOME")
	// Override HOME so JobsDir() resolves to our temp dir.
	os.Setenv("HOME", dir)
	resetDir() // clear cached JobsDir/EnsureDir
	// Create the jobs dir structure.
	os.MkdirAll(filepath.Join(dir, ".config", "quancode", "jobs"), 0755)
	return dir, func() {
		os.Setenv("HOME", orig)
		resetDir()
	}
}

func TestWriteAndReadState(t *testing.T) {
	_, cleanup := setupTestDir(t)
	defer cleanup()

	state := &State{
		JobID:            "job_20260330T100000Z_abcd1234",
		Agent:            "codex",
		Task:             "fix bug",
		WorkDir:          "/tmp/test",
		Isolation:        "worktree",
		EffectiveTimeout: 300,
		Status:           StatusPending,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	if err := WriteState(state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	if state.StatusVersion != 1 {
		t.Errorf("expected status_version=1, got %d", state.StatusVersion)
	}

	got, err := ReadState("job_20260330T100000Z_abcd1234")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}

	if got.Agent != "codex" {
		t.Errorf("expected agent=codex, got %s", got.Agent)
	}
	if got.Status != StatusPending {
		t.Errorf("expected status=pending, got %s", got.Status)
	}
	if got.SchemaVersion != SchemaVersion {
		t.Errorf("expected schema_version=%d, got %d", SchemaVersion, got.SchemaVersion)
	}
}

func TestTerminalStatusCannotBeOverwritten(t *testing.T) {
	_, cleanup := setupTestDir(t)
	defer cleanup()

	state := &State{
		JobID:            "job_20260330T100000Z_term0001",
		Agent:            "codex",
		Task:             "test",
		WorkDir:          "/tmp",
		Isolation:        "worktree",
		EffectiveTimeout: 300,
		Status:           StatusPending,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	// Write pending.
	if err := WriteState(state); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	// Transition to running.
	state.Status = StatusRunning
	if err := WriteState(state); err != nil {
		t.Fatalf("write running: %v", err)
	}

	// Transition to succeeded (terminal).
	state.Status = StatusSucceeded
	if err := WriteState(state); err != nil {
		t.Fatalf("write succeeded: %v", err)
	}

	// Attempt to overwrite terminal status — should fail.
	state.Status = StatusCancelled
	err := WriteState(state)
	if err == nil {
		t.Fatal("expected error overwriting terminal status, got nil")
	}
}

func TestCASVersionConflict(t *testing.T) {
	_, cleanup := setupTestDir(t)
	defer cleanup()

	state := &State{
		JobID:            "job_20260330T100000Z_cas00001",
		Agent:            "codex",
		Task:             "test",
		WorkDir:          "/tmp",
		Isolation:        "worktree",
		EffectiveTimeout: 300,
		Status:           StatusPending,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	if err := WriteState(state); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// state.StatusVersion is now 1.

	// Simulate a stale writer with old version.
	stale := &State{
		JobID:            "job_20260330T100000Z_cas00001",
		Status:           StatusRunning,
		StatusVersion:    0, // stale
		EffectiveTimeout: 300,
	}

	err := WriteState(stale)
	if err == nil {
		t.Fatal("expected CAS conflict error, got nil")
	}
}

func TestWriteStateFailurePath(t *testing.T) {
	_, cleanup := setupTestDir(t)
	defer cleanup()

	state := &State{
		JobID:            "job_20260330T100000Z_fail0001",
		Agent:            "codex",
		Task:             "test write failure",
		WorkDir:          "/tmp",
		Isolation:        "worktree",
		EffectiveTimeout: 300,
		Status:           StatusPending,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	if err := WriteState(state); err != nil {
		t.Fatalf("initial write: %v", err)
	}
	if state.StatusVersion != 1 {
		t.Fatalf("expected initial status_version=1, got %d", state.StatusVersion)
	}

	// Force tmp file creation to fail so WriteState exits after cloning state
	// but before any on-disk update or caller mutation.
	tmpPath := StatePath(state.JobID) + ".tmp"
	if err := os.Mkdir(tmpPath, 0755); err != nil {
		t.Fatalf("mkdir tmp path: %v", err)
	}
	defer os.RemoveAll(tmpPath)

	state.Status = StatusRunning
	err := WriteState(state)
	if err == nil {
		t.Fatal("expected write failure, got nil")
	}

	if state.StatusVersion != 1 {
		t.Fatalf("status_version should remain 1 after failed write, got %d", state.StatusVersion)
	}

	got, err := ReadState(state.JobID)
	if err != nil {
		t.Fatalf("ReadState after failed write: %v", err)
	}
	if got.StatusVersion != 1 {
		t.Fatalf("on-disk status_version should remain 1, got %d", got.StatusVersion)
	}
	if got.Status != StatusPending {
		t.Fatalf("on-disk status should remain pending, got %s", got.Status)
	}
}

func TestListJobsFilterByWorkDir(t *testing.T) {
	_, cleanup := setupTestDir(t)
	defer cleanup()

	// Create two jobs in different workdirs.
	for _, tc := range []struct {
		id      string
		workDir string
	}{
		{"job_20260330T100000Z_list0001", "/project/a"},
		{"job_20260330T100001Z_list0002", "/project/b"},
		{"job_20260330T100002Z_list0003", "/project/a"},
	} {
		state := &State{
			JobID:            tc.id,
			Agent:            "codex",
			Task:             "test",
			WorkDir:          tc.workDir,
			Isolation:        "worktree",
			EffectiveTimeout: 300,
			Status:           StatusPending,
			CreatedAt:        time.Now().UTC().Format(time.RFC3339),
		}
		if err := WriteState(state); err != nil {
			t.Fatalf("write %s: %v", tc.id, err)
		}
	}

	// List all.
	all, err := ListJobs("", 0)
	if err != nil {
		t.Fatalf("ListJobs all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 jobs, got %d", len(all))
	}

	// Filter by workdir.
	filtered, err := ListJobs("/project/a", 0)
	if err != nil {
		t.Fatalf("ListJobs filtered: %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("expected 2 jobs for /project/a, got %d", len(filtered))
	}

	// Test limit.
	limited, err := ListJobs("", 2)
	if err != nil {
		t.Fatalf("ListJobs limited: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("expected 2 jobs with limit, got %d", len(limited))
	}
}

func TestCleanExpiredJobs(t *testing.T) {
	_, cleanup := setupTestDir(t)
	defer cleanup()

	now := time.Now().UTC()
	oldTime := now.Add(-48 * time.Hour).Format(time.RFC3339)
	recentTime := now.Add(-1 * time.Hour).Format(time.RFC3339)

	// Create an old terminal job.
	old := &State{
		JobID:            "job_20260328T100000Z_old00001",
		Agent:            "codex",
		Task:             "old task",
		WorkDir:          "/tmp",
		Isolation:        "worktree",
		EffectiveTimeout: 300,
		Status:           StatusPending,
		CreatedAt:        oldTime,
	}
	if err := WriteState(old); err != nil {
		t.Fatalf("write old: %v", err)
	}
	old.Status = StatusSucceeded
	old.FinishedAt = oldTime
	if err := WriteState(old); err != nil {
		t.Fatalf("write old terminal: %v", err)
	}

	// Create a recent terminal job (within protection period).
	recent := &State{
		JobID:            "job_20260330T090000Z_new00001",
		Agent:            "codex",
		Task:             "recent task",
		WorkDir:          "/tmp",
		Isolation:        "worktree",
		EffectiveTimeout: 300,
		Status:           StatusPending,
		CreatedAt:        recentTime,
	}
	if err := WriteState(recent); err != nil {
		t.Fatalf("write recent: %v", err)
	}
	recent.Status = StatusSucceeded
	recent.FinishedAt = recentTime
	if err := WriteState(recent); err != nil {
		t.Fatalf("write recent terminal: %v", err)
	}

	// Create a running job (should not be cleaned).
	running := &State{
		JobID:            "job_20260329T100000Z_run00001",
		Agent:            "codex",
		Task:             "running task",
		WorkDir:          "/tmp",
		Isolation:        "worktree",
		EffectiveTimeout: 300,
		Status:           StatusPending,
		CreatedAt:        oldTime,
	}
	if err := WriteState(running); err != nil {
		t.Fatalf("write running: %v", err)
	}
	running.Status = StatusRunning
	running.PID = 99999 // fake pid
	if err := WriteState(running); err != nil {
		t.Fatalf("write running status: %v", err)
	}

	// Create an orphan output file with old modtime.
	orphanPath := filepath.Join(JobsDir(), "job_20260301T000000Z_orphan01.output")
	os.WriteFile(orphanPath, []byte("orphan"), 0644)
	oldModTime := time.Now().Add(-72 * time.Hour)
	os.Chtimes(orphanPath, oldModTime, oldModTime)

	// Clean with TTL=24h, protection=30s.
	result, err := Clean(24*time.Hour, 30*time.Second)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}

	if result.Removed != 1 {
		t.Errorf("expected 1 removed, got %d", result.Removed)
	}

	// Recent should still exist.
	if _, err := ReadState("job_20260330T090000Z_new00001"); err != nil {
		t.Errorf("recent job should still exist: %v", err)
	}

	// Old should be gone.
	if _, err := os.Stat(StatePath("job_20260328T100000Z_old00001")); !os.IsNotExist(err) {
		t.Error("old job state file should be removed")
	}

	// Orphan should be gone.
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Error("orphan output file should be removed")
	}
}

func TestNewJobID(t *testing.T) {
	id, err := NewJobID()
	if err != nil {
		t.Fatalf("NewJobID: %v", err)
	}

	if len(id) < 20 {
		t.Errorf("job id too short: %s", id)
	}

	if id[:4] != "job_" {
		t.Errorf("expected job_ prefix, got %s", id[:4])
	}

	// Verify uniqueness.
	id2, _ := NewJobID()
	if id == id2 {
		t.Error("two job IDs should not be equal")
	}
}

func TestIsTerminal(t *testing.T) {
	terminals := []string{StatusSucceeded, StatusFailed, StatusTimedOut, StatusCancelled, StatusLost}
	for _, s := range terminals {
		if !IsTerminal(s) {
			t.Errorf("%s should be terminal", s)
		}
	}

	nonTerminals := []string{StatusPending, StatusRunning}
	for _, s := range nonTerminals {
		if IsTerminal(s) {
			t.Errorf("%s should not be terminal", s)
		}
	}
}
