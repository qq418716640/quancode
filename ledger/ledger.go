package ledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Entry represents a single delegation attempt record.
type Entry struct {
	Timestamp      string          `json:"timestamp"`
	Agent          string          `json:"agent"`
	Task           string          `json:"task"`
	ExitCode       int             `json:"exit_code"`
	TimedOut       bool            `json:"timed_out"`
	DurationMs     int64           `json:"duration_ms"`
	ChangedFiles   []string        `json:"changed_files,omitempty"`
	ApprovalEvents []ApprovalEvent `json:"approval_events,omitempty"`
	Isolation      string          `json:"isolation,omitempty"`
	WorkDir        string          `json:"work_dir"`
	FinalStatus    string          `json:"final_status,omitempty"`

	// Run/attempt tracking — links multiple attempts within a single delegate invocation.
	RunID          string `json:"run_id,omitempty"`
	Attempt        int    `json:"attempt,omitempty"`
	FallbackFrom   string `json:"fallback_from,omitempty"`
	FallbackReason string `json:"fallback_reason,omitempty"`

	// Failure classification and patch conflict details
	FailureClass  string   `json:"failure_class,omitempty"`
	ConflictFiles []string `json:"conflict_files,omitempty"`

	// VerifyRaw stores the verification result as raw JSON to avoid
	// a circular dependency between ledger and cmd packages.
	VerifyRaw json.RawMessage `json:"verify,omitempty"`
}

type ApprovalEvent struct {
	RequestID   string `json:"request_id"`
	Action      string `json:"action"`
	Description string `json:"description"`
	Decision    string `json:"decision,omitempty"`
}

// LogDir returns the path to the ledger log directory.
func LogDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "quancode", "logs")
	}
	return filepath.Join(".", ".quancode", "logs")
}

// logFilePath returns the path for today's log file.
func logFilePath() string {
	date := time.Now().Format("2006-01-02")
	return filepath.Join(LogDir(), date+".jsonl")
}

// Append writes an entry to today's log file (one JSON line per entry).
func Append(entry *Entry) error {
	dir := LogDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}

	f, err := os.OpenFile(logFilePath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write entry: %w", err)
	}

	return nil
}

// ReadAll reads all entries from all log files.
func ReadAll() ([]Entry, error) {
	dir := LogDir()
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("glob log files: %w", err)
	}

	var entries []Entry
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range splitNonEmpty(data) {
			var entry Entry
			if err := json.Unmarshal([]byte(line), &entry); err == nil {
				entries = append(entries, entry)
			}
		}
	}

	return entries, nil
}

// ReadSince reads entries from a given time onward.
func ReadSince(since time.Time) ([]Entry, error) {
	all, err := ReadAll()
	if err != nil {
		return nil, err
	}

	var filtered []Entry
	for _, e := range all {
		t, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			continue
		}
		if !t.Before(since) {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}

func splitNonEmpty(data []byte) []string {
	var lines []string
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			line := string(data[start:i])
			if line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(data) {
		line := string(data[start:])
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
