package ledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/qq418716640/quancode/version"
)

// Entry represents a single delegation attempt record.
type Entry struct {
	Timestamp    string   `json:"timestamp"`
	Agent        string   `json:"agent"`
	Task         string   `json:"task"`
	ExitCode     int      `json:"exit_code"`
	TimedOut     bool     `json:"timed_out"`
	Cancelled    bool     `json:"cancelled,omitempty"`
	DurationMs   int64    `json:"duration_ms"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	Isolation    string   `json:"isolation,omitempty"`
	WorkDir      string   `json:"work_dir"`
	FinalStatus  string   `json:"final_status,omitempty"`

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
	CancelledBy     string `json:"cancelled_by,omitempty"`     // deprecated: retained for reading old data
	Selected        bool   `json:"selected,omitempty"`         // true if this was the chosen result in speculative execution
	SelectionReason string `json:"selection_reason,omitempty"` // "primary_preferred" or "primary_failed"

	// Pipeline tracking — links stages in a pipeline execution.
	PipelineID   string `json:"pipeline_id,omitempty"`
	PipelineName string `json:"pipeline_name,omitempty"`
	StageName    string `json:"stage_name,omitempty"`
	StageIndex   int    `json:"stage_index,omitempty"`

	// ReviewSetID links sibling delegations from a single `review-set`
	// invocation (fan-out to N agents over the same task). Each agent's
	// delegation gets its own RunID/DelegationID; ReviewSetID groups them.
	ReviewSetID string `json:"review_set_id,omitempty"`

	// Batch tracking — links attempts produced by `quancode batch`.
	// BatchItemID is the stable item identifier, formatted as a zero-padded
	// index plus a content hash ("0007-a1b2c3d4"). The item text itself is
	// not recorded here: it may repeat or contain arbitrary characters, and
	// the rendered task is already in Task.
	BatchID     string `json:"batch_id,omitempty"`
	BatchItemID string `json:"batch_item_id,omitempty"`

	// DelegationID uniquely identifies each delegation attempt.
	DelegationID string `json:"delegation_id,omitempty"`

	// OutputFile is the path to the file storing agent output for this delegation.
	OutputFile string `json:"output_file,omitempty"`

	// VerifyRaw stores the verification result as raw JSON to avoid
	// a circular dependency between ledger and cmd packages.
	VerifyRaw json.RawMessage `json:"verify,omitempty"`

	// Version records the quancode version that produced this entry.
	Version string `json:"version,omitempty"`

	// MatchedHints lists diagnostic hint messages that matched the failure
	// output of this attempt. Populated from runner.Result.MatchedHints.
	// Empty/omitted on success or when no patterns matched.
	MatchedHints []string `json:"matched_hints,omitempty"`

	// AgentFault marks a failure that indicts the agent itself (quota
	// exhaustion, upstream rejection, expired login, unknown model) rather
	// than the task, the working directory, or QuanCode's own orchestration.
	// The health breaker counts only these. Derived from the matched
	// failure pattern at write time so health can be computed from the
	// ledger alone, without re-reading output files.
	AgentFault bool `json:"agent_fault,omitempty"`

	// CostUSD, TokensIn, TokensOut, and AgentSessionID are populated when the
	// agent's ResultFormat config opts into structured output parsing
	// (currently: claude's json_object, codex's jsonl_events — see
	// config.AgentConfig.ResultFormat). Pointers so "this CLI doesn't report
	// cost" (nil/absent) is distinguishable from "reported as exactly $0"
	// (present, zero). Populated whether the attempt succeeded or failed —
	// failed attempts can still have consumed tokens and money.
	CostUSD        *float64 `json:"cost_usd,omitempty"`
	TokensIn       *int64   `json:"tokens_in,omitempty"`
	TokensOut      *int64   `json:"tokens_out,omitempty"`
	AgentSessionID string   `json:"agent_session_id,omitempty"`

	// Deprecations records lifecycle warnings seen on the agent's stderr,
	// one entry per matched line. Recorded here because stderr is captured
	// into a buffer and discarded on success — the ledger is the only place
	// a warning that fires on a *working* delegation can survive to be
	// noticed. `quancode doctor` reads them back.
	Deprecations []string `json:"deprecations,omitempty"`

	// TaskSizeWarning is set when the user task length exceeded the
	// configured warning threshold (preferences.task_size_warn_threshold).
	// Empty when under threshold or warnings are disabled. Used by
	// dashboard and post-hoc analysis to find oversized tasks correlated
	// with timeouts.
	TaskSizeWarning string `json:"task_size_warning,omitempty"`
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
//
// Log files are named {date}.jsonl, so files whose date is entirely older
// than `since` are skipped without being opened. This keeps hot-path callers
// (the health breaker runs on every delegation) from parsing months of
// history to answer a question about the last day.
func ReadSince(since time.Time) ([]Entry, error) {
	dir := LogDir()
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("glob log files: %w", err)
	}

	// Entries carry UTC timestamps while file names use local dates. Step
	// back a day so a file straddling the boundary is never skipped.
	cutoffDate := since.Add(-24 * time.Hour).Format("2006-01-02")

	var filtered []Entry
	for _, f := range files {
		if name := strings.TrimSuffix(filepath.Base(f), ".jsonl"); name < cutoffDate {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range splitNonEmpty(data) {
			var entry Entry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue
			}
			t, err := time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil || t.Before(since) {
				continue
			}
			filtered = append(filtered, entry)
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
