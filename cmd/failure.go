package cmd

// Failure class values recorded in ledger entries.
const (
	FailureClassLaunchFailure = "launch_failure"
	FailureClassTimedOut      = "timed_out"
	FailureClassRateLimited   = "rate_limited"
	FailureClassAgentFailed   = "agent_failed"
	FailureClassPatchConflict = "patch_conflict"
	FailureClassVerifyFailed  = "verify_failed"
)

// classifyFailure determines the failure class for a delegation attempt.
// Returns empty string for successful attempts.
func classifyFailure(ar attemptResult) string {
	// Launch failure: agent process never started
	if ar.result == nil && ar.err != nil {
		return FailureClassLaunchFailure
	}

	// Patch apply failure takes precedence over agent exit code
	// because the agent itself may have succeeded
	if ar.patchApplyErr != nil {
		return FailureClassPatchConflict
	}

	// Verify-strict failure
	if ar.verify.IsStrictFailure() {
		return FailureClassVerifyFailed
	}

	if ar.result == nil {
		return ""
	}

	// Timeout
	if ar.result.TimedOut {
		return FailureClassTimedOut
	}

	// Success
	if ar.result.ExitCode == 0 && ar.err == nil {
		return ""
	}

	// Rate limit / transient error detection
	if isFallbackEligible(ar.result, ar.output, ar.stderr) {
		return FailureClassRateLimited
	}

	// Agent exited non-zero for non-transient reasons
	return FailureClassAgentFailed
}

// isTransientFailure returns true if the failure class represents a
// transient error where retrying with a different agent may help.
func isTransientFailure(class string) bool {
	switch class {
	case FailureClassLaunchFailure, FailureClassTimedOut, FailureClassRateLimited:
		return true
	default:
		return false
	}
}
