package job

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// getProcessStartTime returns the process start time on Linux by reading
// /proc/<pid>/stat. The returned value is the starttime field (field 22),
// measured in clock ticks since system boot.
func getProcessStartTime(pid int) (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}

	// The stat file format has the comm field (field 2) in parentheses,
	// which may contain spaces. Find the last ')' to skip it.
	s := string(data)
	idx := strings.LastIndex(s, ")")
	if idx < 0 {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(s[idx+1:])
	// After the closing paren, fields are 1-indexed starting at field 3.
	// starttime is field 22, so it's at index 22-3 = 19.
	if len(fields) < 20 {
		return 0, fmt.Errorf("/proc/%d/stat: not enough fields", pid)
	}

	startTime, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse starttime: %w", err)
	}

	return startTime, nil
}
