package job

import (
	"os"
	"syscall"
)

// GetProcessStartTime returns the start time for the given PID.
// Exported for use by job-runner to record its own start time.
func GetProcessStartTime(pid int) (int64, error) {
	return getProcessStartTime(pid)
}

// IsProcessAlive checks if a process with the given PID is still running.
// Exported for use by job cancel command.
func IsProcessAlive(pid int, pidStartTime int64) bool {
	return isProcessAlive(pid, pidStartTime)
}

// isProcessAlive checks if a process with the given PID is still running
// and optionally validates its start time to avoid PID reuse false positives.
// If pidStartTime is 0, only the PID existence check is performed.
func isProcessAlive(pid int, pidStartTime int64) bool {
	if pid <= 0 {
		return false
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds. Send signal 0 to check existence.
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		return false
	}

	// If we have a recorded start time, verify it matches to prevent
	// acting on a reused PID.
	if pidStartTime > 0 {
		actual, err := getProcessStartTime(pid)
		if err != nil {
			// Can't verify — assume alive to be conservative.
			return true
		}
		return actual == pidStartTime
	}

	return true
}
