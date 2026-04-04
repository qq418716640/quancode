package ledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/qq418716640/quancode/version"
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
	Isolation string `json:"isolation,omitempty"`
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

	// Speculative parallelism tracking
	Speculative     bool   `json:"speculative,omitempty"`      // true if this attempt was part of speculative execution
	SpeculativeRole string `json:"speculative_role,omitempty"` // "primary" or "speculative"
	CancelledBy     string `json:"cancelled_by,omitempty"`     // agent key that won, causing this one to be cancelled

	// Pipeline tracking — links stages in a pipeline execution.
	PipelineID   string `json:"pipeline_id,omitempty"`
	PipelineName string `json:"pipeline_name,omitempty"`
	StageName    string `json:"stage_name,omitempty"`
	StageIndex   int    `json:"stage_index,omitempty"`

	// DelegationID uniquely identifies each delegation attempt.
	DelegationID string `json:"delegation_id,omitempty"`

	// OutputFile is the path to the file storing agent output for this delegation.
	OutputFile string `json:"output_file,omitempty"`

	// VerifyRaw stores the verification result as raw JSON to avoid
	// a circular dependency between ledger and cmd packages.
	VerifyRaw json.RawMessage `json:"verify,omitempty"`

	// Version records the quancode version that produced this entry.
	Version string `json:"version,omitempty"`
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
	if entry.Version == "" {
		entry.Version = version.Version
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

// OutputDir returns the path to the delegation output directory.
func OutputDir() string {
	return filepath.Join(LogDir(), "outputs")
}

// OutputPath returns the output file path for a given delegation ID.
func OutputPath(delegationID string) string {
	return filepath.Join(OutputDir(), delegationID+".output")
}

// DefaultMaxOutputBytes is the maximum size for delegation output files (50MB).
const DefaultMaxOutputBytes = 50 * 1024 * 1024

var outputDirOnce sync.Once
var outputDirErr error

// WriteOutput writes delegation output to a file, capped at maxBytes.
// Returns the output file path, or empty string if output is empty.
func WriteOutput(delegationID, output string, maxBytes int64) string {
	if output == "" || delegationID == "" {
		return ""
	}

	dir := OutputDir()
	outputDirOnce.Do(func() {
		outputDirErr = os.MkdirAll(dir, 0755)
	})
	if outputDirErr != nil {
		fmt.Fprintf(os.Stderr, "[quancode] warning: create output dir: %v\n", outputDirErr)
		return ""
	}

	path := filepath.Join(dir, filepath.Base(delegationID)+".output")
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[quancode] warning: create output file: %v\n", err)
		return ""
	}
	defer f.Close()

	data := []byte(output)
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		data = data[:maxBytes]
	}
	if _, err := f.Write(data); err != nil {
		fmt.Fprintf(os.Stderr, "[quancode] warning: write output file: %v\n", err)
		return ""
	}
	return path
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
