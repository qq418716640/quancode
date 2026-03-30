package cmd

import (
	"fmt"
	"os"

	"github.com/qq418716640/quancode/runner"
)

// Verify status constants.
const (
	VerifyPassed  = "passed"
	VerifyFailed  = "failed"
	VerifySkipped = "skipped"
	VerifyError   = "error"
	VerifyTimedOut = "timed_out"
)

// Final status constants for delegation results and ledger entries.
const (
	StatusCompleted                      = "completed"
	StatusFailed                         = "failed"
	StatusTimedOut                       = "timed_out"
	StatusCompletedWithVerifyFailures    = "completed_with_verification_failures"
)

// IsStrictFailure returns true if this is a strict verification that failed.
// Safe to call on nil receiver.
func (vr *VerifyResult) IsStrictFailure() bool {
	return vr != nil && vr.Strict && vr.Status == VerifyFailed
}

// VerifyResult holds the aggregated result of running verification commands.
type VerifyResult struct {
	Enabled     bool                  `json:"enabled"`
	Strict      bool                  `json:"strict"`
	Status      string                `json:"status"` // "passed", "failed", "skipped"
	Commands    []string              `json:"commands,omitempty"`
	Results     []VerifyCommandResult `json:"results,omitempty"`
	PassedCount int                   `json:"passed_count"`
	FailedCount int                   `json:"failed_count"`
}

// VerifyCommandResult holds the result of a single verification command.
type VerifyCommandResult struct {
	Command    string `json:"command"`
	ExitCode   int    `json:"exit_code"`
	TimedOut   bool   `json:"timed_out"`
	DurationMs int64  `json:"duration_ms"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Status     string `json:"status"` // "passed", "failed", "error", "timed_out"
	Error      string `json:"error,omitempty"`
}

// runVerifyCommand is the function used to execute verification commands.
// It can be replaced in tests.
var runVerifyCommand = func(workDir string, timeoutSecs int, env []string, name string, args ...string) (*runner.Result, error) {
	return runner.Run(workDir, timeoutSecs, env, name, args...)
}

// runVerification executes verification commands in the given directory.
// Commands are run sequentially; results are aggregated.
// This function does not produce any stderr output; use runAndLogVerification
// for the version that logs progress.
func runVerification(execDir string, cmds []string, timeoutSecs int, strict bool) *VerifyResult {
	if len(cmds) == 0 {
		return &VerifyResult{Status: VerifySkipped}
	}

	vr := &VerifyResult{
		Enabled:  true,
		Strict:   strict,
		Commands: cmds,
	}

	for _, cmdStr := range cmds {
		vcr := runSingleVerify(execDir, cmdStr, timeoutSecs)
		vr.Results = append(vr.Results, vcr)
		if vcr.Status == VerifyPassed {
			vr.PassedCount++
		} else {
			vr.FailedCount++
		}
	}

	if vr.FailedCount > 0 {
		vr.Status = VerifyFailed
	} else {
		vr.Status = VerifyPassed
	}

	return vr
}

// runAndLogVerification runs verification and logs the result to stderr.
func runAndLogVerification(execDir string, vs *verifySpec) *VerifyResult {
	if vs == nil || len(vs.Commands) == 0 {
		return nil
	}
	vr := runVerification(execDir, vs.Commands, vs.TimeoutSec, vs.Strict)
	if vr.Status == VerifyFailed {
		if vs.Strict {
			fmt.Fprintf(os.Stderr, "[quancode] verify-strict failed: %d/%d commands failed\n", vr.FailedCount, len(vs.Commands))
		} else {
			fmt.Fprintf(os.Stderr, "[quancode] verify: %d/%d commands failed (recorded)\n", vr.FailedCount, len(vs.Commands))
		}
	} else {
		fmt.Fprintf(os.Stderr, "[quancode] verify: all %d commands passed\n", vr.PassedCount)
	}
	return vr
}

// determineFinalStatus computes the final status string from agent result and verification.
// Agent execution result takes priority: timeout/failure are reported as-is.
// Verification only affects status when the agent itself succeeded.
func determineFinalStatus(exitCode int, timedOut bool, verify *VerifyResult) string {
	if timedOut {
		return StatusTimedOut
	}
	if exitCode != 0 {
		return StatusFailed
	}
	// Agent succeeded — now check verification
	if verify != nil && verify.Enabled && verify.Status == VerifyFailed {
		if verify.Strict {
			return StatusFailed
		}
		return StatusCompletedWithVerifyFailures
	}
	return StatusCompleted
}

const maxVerifyOutputBytes = 64 * 1024 // 64KB per stdout/stderr

func truncateOutput(s string) string {
	if len(s) <= maxVerifyOutputBytes {
		return s
	}
	return s[:maxVerifyOutputBytes] + "\n... [output truncated]\n"
}

func runSingleVerify(execDir, cmdStr string, timeoutSecs int) VerifyCommandResult {
	vcr := VerifyCommandResult{Command: cmdStr}

	result, err := runVerifyCommand(execDir, timeoutSecs, nil, "sh", "-c", cmdStr)
	if err != nil && result == nil {
		vcr.Status = VerifyError
		vcr.Error = err.Error()
		vcr.ExitCode = 1
		return vcr
	}

	if result != nil {
		vcr.ExitCode = result.ExitCode
		vcr.TimedOut = result.TimedOut
		vcr.DurationMs = result.DurationMs
		vcr.Stdout = truncateOutput(result.Stdout)
		vcr.Stderr = truncateOutput(result.Stderr)

		switch {
		case result.TimedOut:
			vcr.Status = VerifyTimedOut
		case result.ExitCode == 0:
			vcr.Status = VerifyPassed
		default:
			vcr.Status = VerifyFailed
		}
	}

	return vcr
}
