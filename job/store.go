package job

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
)

var (
	jobsDirOnce  sync.Once
	jobsDirValue string

	ensureDirOnce sync.Once
	ensureDirErr  error
)

// JobsDir returns the directory where job state files are stored.
// The result is computed once and cached.
func JobsDir() string {
	jobsDirOnce.Do(func() {
		if home, err := os.UserHomeDir(); err == nil {
			jobsDirValue = filepath.Join(home, ".config", "quancode", "jobs")
		} else {
			jobsDirValue = filepath.Join(".", ".quancode", "jobs")
		}
	})
	return jobsDirValue
}

// StatePath returns the absolute path for a job's state file.
func StatePath(jobID string) string {
	return filepath.Join(JobsDir(), jobID+".json")
}

// OutputPath returns the absolute path for a job's output file.
func OutputPath(jobID string) string {
	return filepath.Join(JobsDir(), jobID+".output")
}

// PatchPath returns the absolute path for a job's patch file.
func PatchPath(jobID string) string {
	return filepath.Join(JobsDir(), jobID+".patch")
}

// EnsureDir creates the jobs directory if it doesn't exist.
// The directory is created at most once per process.
func EnsureDir() error {
	ensureDirOnce.Do(func() {
		ensureDirErr = os.MkdirAll(JobsDir(), 0755)
	})
	return ensureDirErr
}

// resetDir resets the cached directory for testing.
func resetDir() {
	jobsDirOnce = sync.Once{}
	ensureDirOnce = sync.Once{}
	ensureDirErr = nil
}

// WriteState persists a job state to disk with flock + CAS + atomic rename.
// It enforces: terminal statuses cannot be overwritten, and status_version
// must match the on-disk value (optimistic concurrency).
// On success, state.StatusVersion is incremented. On failure, state is not modified.
func WriteState(state *State) error {
	if err := EnsureDir(); err != nil {
		return fmt.Errorf("ensure jobs dir: %w", err)
	}

	path := StatePath(state.JobID)

	// Lock file is persistent (not deleted after use) to prevent flock races.
	// flock locks inodes, not paths — deleting the file would let a concurrent
	// process create a new inode and acquire a second lock.
	lockPath := path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	// Read current state for CAS validation.
	existing, err := readStateFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing state: %w", err)
	}

	if existing != nil {
		// Terminal status cannot be overwritten.
		if IsTerminal(existing.Status) {
			return fmt.Errorf("job %s is in terminal status %q, cannot update", state.JobID, existing.Status)
		}
		// CAS: status_version must match.
		if state.StatusVersion != existing.StatusVersion {
			return fmt.Errorf("job %s: status_version conflict (expected %d, got %d)",
				state.JobID, existing.StatusVersion, state.StatusVersion)
		}
	}

	// Work on a copy to avoid polluting caller's state on failure.
	toWrite := *state
	toWrite.StatusVersion++
	toWrite.SchemaVersion = SchemaVersion

	data, err := json.MarshalIndent(&toWrite, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// Atomic write: write to tmp, fsync, then rename.
	tmpPath := path + ".tmp"
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create tmp file: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write tmp file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("fsync tmp file: %w", err)
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename tmp to state: %w", err)
	}

	// Success — update caller's state to reflect the persisted version.
	state.StatusVersion = toWrite.StatusVersion
	state.SchemaVersion = toWrite.SchemaVersion

	return nil
}

// ReadState reads a job state from disk. This is a pure read with no side effects.
// Use DetectLost to check and mark jobs whose runner process has died.
func ReadState(jobID string) (*State, error) {
	path := StatePath(jobID)
	state, err := readStateFile(path)
	if err != nil {
		return nil, err
	}

	if state.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("job %s: schema_version %d is newer than supported %d, please upgrade quancode",
			jobID, state.SchemaVersion, SchemaVersion)
	}

	return state, nil
}

// DetectLost checks if a non-terminal job's runner process is still alive.
// If the process is gone, it attempts to mark the job as lost on disk
// (best-effort) and returns the updated state. The input state is not modified.
func DetectLost(state *State) *State {
	if IsTerminal(state.Status) || state.PID <= 0 {
		return state
	}
	if isProcessAlive(state.PID, state.PIDStartTime) {
		return state
	}
	lost := *state
	lost.Status = StatusLost
	lost.ErrorCode = ErrCodeUnknown
	lost.Error = "job-runner process no longer exists"
	_ = WriteState(&lost) // best-effort
	return &lost
}

// ListJobs returns all job states, optionally filtered by workdir.
// Results are sorted newest first (by job_id time prefix).
// Unreadable state files are skipped but do not cause the call to fail.
// Note: this function calls DetectLost which may write to disk (best-effort).
func ListJobs(workDir string, limit int) ([]*State, error) {
	dir := JobsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read jobs dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	var jobs []*State
	for i := len(files) - 1; i >= 0; i-- {
		name := files[i]
		jobID := strings.TrimSuffix(name, ".json")
		state, err := ReadState(jobID)
		if err != nil {
			continue
		}
		state = DetectLost(state)
		if workDir != "" && state.WorkDir != workDir {
			continue
		}
		jobs = append(jobs, state)
		if limit > 0 && len(jobs) >= limit {
			break
		}
	}

	return jobs, nil
}

// readStateFile reads and unmarshals a state file without any side effects.
func readStateFile(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &state, nil
}
