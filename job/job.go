package job

import (
	"encoding/json"
)

// SchemaVersion is the current version of the job state file format.
// Newer quancode reads older schemas by filling zero values.
// Older quancode encountering a newer schema_version should report an error.
const SchemaVersion = 1

// Job status constants.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusTimedOut  = "timed_out"
	StatusCancelled = "cancelled"
	StatusLost      = "lost"
)

// terminalStatuses is the set of statuses that cannot be overwritten.
var terminalStatuses = map[string]bool{
	StatusSucceeded: true,
	StatusFailed:    true,
	StatusTimedOut:  true,
	StatusCancelled: true,
	StatusLost:      true,
}

// IsTerminal reports whether a status is a terminal (final) state.
func IsTerminal(status string) bool {
	return terminalStatuses[status]
}

// Error code constants for structured failure classification.
const (
	ErrCodeSpawnFailed  = "spawn_failed"
	ErrCodeRouteFailed  = "route_failed"
	ErrCodeAgentError   = "agent_error"
	ErrCodeVerifyFailed = "verify_failed"
	ErrCodeTimeout      = "timeout"
	ErrCodeCancelled    = "cancelled"
	ErrCodeWorktree     = "worktree_error"
	ErrCodeUnknown      = "unknown"
)

// State represents a persisted async job.
type State struct {
	SchemaVersion    int             `json:"schema_version"`
	JobID            string          `json:"job_id"`
	Agent            string          `json:"agent"`
	ActualAgent      string          `json:"actual_agent,omitempty"`
	Task             string          `json:"task"`
	WorkDir          string          `json:"workdir"`
	PID              int             `json:"pid,omitempty"`
	PIDStartTime     int64           `json:"pid_start_time,omitempty"`
	Isolation        string          `json:"isolation"`
	RequestedTimeout int             `json:"requested_timeout,omitempty"`
	EffectiveTimeout int             `json:"effective_timeout"`
	Status           string          `json:"status"`
	StatusVersion    int64           `json:"status_version"`
	ErrorCode        string          `json:"error_code,omitempty"`
	Error            string          `json:"error,omitempty"`
	CreatedAt        string          `json:"created_at"`
	CheckpointAt     string          `json:"checkpoint_at,omitempty"`
	FinishedAt       string          `json:"finished_at,omitempty"`
	OutputFile       string          `json:"output_file,omitempty"`
	PatchFile        string          `json:"patch_file,omitempty"`
	ChangedFiles     []string        `json:"changed_files,omitempty"`
	ExitCode         *int            `json:"exit_code,omitempty"`
	RunnerVersion    string          `json:"runner_version,omitempty"`
	LedgerWrittenAt  string          `json:"ledger_written_at,omitempty"`
	DelegationID     string          `json:"delegation_id,omitempty"`
	RunID            string          `json:"run_id,omitempty"`
	VerifyRaw        json.RawMessage `json:"verify,omitempty"`
}
