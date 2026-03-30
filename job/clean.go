package job

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CleanResult holds the outcome of a clean operation.
type CleanResult struct {
	Removed int
	Skipped int
	Errors  []string
}

// Clean removes expired job files and their associated artifacts.
// Only jobs in terminal status are eligible for removal.
// Jobs finished within protectionPeriod are skipped.
// Also removes orphan files (output/patch/lock/tmp) whose job state is missing.
func Clean(ttl, protectionPeriod time.Duration) (*CleanResult, error) {
	dir := JobsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &CleanResult{}, nil
		}
		return nil, fmt.Errorf("read jobs dir: %w", err)
	}

	now := time.Now()
	result := &CleanResult{}

	// Collect all job IDs that have a state file.
	stateJobIDs := make(map[string]bool)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			id := strings.TrimSuffix(e.Name(), ".json")
			stateJobIDs[id] = true
		}
	}

	// Phase 1: clean expired terminal jobs.
	for id := range stateJobIDs {
		state, err := readStateFile(StatePath(id))
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("read %s: %v", id, err))
			continue
		}

		if !IsTerminal(state.Status) {
			result.Skipped++
			continue
		}

		// Parse finished_at to check TTL and protection period.
		finishedStr := state.FinishedAt
		if finishedStr == "" {
			finishedStr = state.CreatedAt
		}
		finishedAt, err := time.Parse(time.RFC3339, finishedStr)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("parse time for %s: %v", id, err))
			continue
		}

		// Protection period: don't clean recently finished jobs.
		if now.Sub(finishedAt) < protectionPeriod {
			result.Skipped++
			continue
		}

		// TTL check.
		if now.Sub(finishedAt) < ttl {
			result.Skipped++
			continue
		}

		// Remove all files for this job.
		if err := removeJobFiles(dir, id); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("remove %s: %v", id, err))
			continue
		}
		result.Removed++
	}

	// Phase 2: scan for orphan files (output/patch/lock/tmp with no state file).
	// Only remove orphans older than the TTL to avoid deleting files belonging
	// to jobs that are still starting up (state file not yet written).
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()

		// Skip state files (.json) and lock files (lock files are persistent,
		// cleaned only when their parent job is removed in phase 1).
		if strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".json.tmp") || strings.HasSuffix(name, ".lock") {
			continue
		}

		jobID := extractJobID(name)
		if jobID == "" {
			continue
		}

		// If no state file exists for this job ID, check age before removing.
		if !stateJobIDs[jobID] {
			fullPath := filepath.Join(dir, name)
			info, err := e.Info()
			if err != nil {
				continue
			}
			// Only remove orphans older than TTL.
			if now.Sub(info.ModTime()) < ttl {
				continue
			}
			if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
				result.Errors = append(result.Errors, fmt.Sprintf("remove orphan %s: %v", name, err))
			}
		}
	}

	return result, nil
}

// knownExtensions are the file extensions managed by the job system.
// Used by both removeJobFiles and extractJobID.
var knownExtensions = []string{".json", ".json.tmp", ".output", ".patch", ".lock"}

// removeJobFiles deletes all files associated with a job ID.
func removeJobFiles(dir, jobID string) error {
	var firstErr error
	for _, ext := range knownExtensions {
		path := filepath.Join(dir, jobID+ext)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// extractJobID extracts the job ID from a filename like "job_20260330T100000Z_a1b2c3d4.output".
// Returns empty string if the filename doesn't look like a job file.
func extractJobID(name string) string {
	if !strings.HasPrefix(name, "job_") {
		return ""
	}
	for _, ext := range knownExtensions {
		if strings.HasSuffix(name, ext) {
			return strings.TrimSuffix(name, ext)
		}
	}
	return ""
}
