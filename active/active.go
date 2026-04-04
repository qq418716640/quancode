// Package active tracks currently running synchronous delegations via lightweight
// marker files in ~/.config/quancode/active/. Async delegations are excluded
// because they already have persistent state in the job/ package.
package active

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/qq418716640/quancode/job"
)

// Entry represents a running synchronous delegation.
type Entry struct {
	DelegationID string `json:"delegation_id"`
	Agent        string `json:"agent"`
	Task         string `json:"task"`
	WorkDir      string `json:"workdir"`
	PID          int    `json:"pid"`
	PIDStartTime int64  `json:"pid_start_time"`
	StartedAt    string `json:"started_at"`
}

var (
	dirOnce  sync.Once
	dirValue string
)

// Dir returns the active tasks directory path.
func Dir() string {
	dirOnce.Do(func() {
		if home, err := os.UserHomeDir(); err == nil {
			dirValue = filepath.Join(home, ".config", "quancode", "active")
		} else {
			dirValue = filepath.Join(".", ".quancode", "active")
		}
	})
	return dirValue
}

// Register writes a marker file for a running delegation.
// Errors are silently ignored to avoid impacting the main delegation flow.
func Register(delegationID, agent, taskText, workDir string) {
	dir := Dir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	pid := os.Getpid()
	startTime, _ := job.GetProcessStartTime(pid)

	// Truncate task to keep the file small
	task := taskText
	if len(task) > 200 {
		task = task[:200]
	}

	entry := Entry{
		DelegationID: delegationID,
		Agent:        agent,
		Task:         task,
		WorkDir:      workDir,
		PID:          pid,
		PIDStartTime: startTime,
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	path := filepath.Join(dir, delegationID+".json")
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmpPath, path)
}

// Unregister removes the marker file for a completed delegation.
// Errors are silently ignored.
func Unregister(delegationID string) {
	path := filepath.Join(Dir(), delegationID+".json")
	_ = os.Remove(path)
}

// List returns all active (alive) entries, cleaning up stale ones.
func List() []Entry {
	dir := Dir()
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var entries []Entry
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}

		path := filepath.Join(dir, f.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var entry Entry
		if err := json.Unmarshal(data, &entry); err != nil {
			_ = os.Remove(path) // corrupted file
			continue
		}

		if !job.IsProcessAlive(entry.PID, entry.PIDStartTime) {
			_ = os.Remove(path) // stale entry
			continue
		}

		entries = append(entries, entry)
	}

	return entries
}
