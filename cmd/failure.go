package cmd

// Failure class values recorded in ledger entries.
const (
	FailureClassLaunchFailure        = "launch_failure"
	FailureClassTimedOut             = "timed_out"
	FailureClassRateLimited          = "rate_limited"
	FailureClassAgentFailed          = "agent_failed"
	FailureClassPatchConflict        = "patch_conflict"
	FailureClassVerifyFailed         = "verify_failed"
	FailureClassSpeculativeCancelled = "speculative_cancelled"
	FailureClassTemplateError        = "template_error"
	// FailureClassOrchestrationError covers failures that happen before the
	// agent process is launched: worktree creation, context-diff apply, ID
	// generation. Split out of launch_failure in v0.9.0 so a git problem in
	// the working directory can no longer be blamed on the agent.
	FailureClassOrchestrationError = "orchestration_error"
)

// classifyFailure determines the failure class for a delegation attempt.
// Returns empty string for successful attempts.
func classifyFailure(ar attemptResult) string {
	// Setup failed before the agent was ever launched — not the agent's fault.
	if ar.orchestrationErr {
		return FailureClassOrchestrationError
	}

	// Launch failure: the agent process never started. runCmd returns a
	// non-nil Result with ExitCode 0 alongside an error when cmd.Run fails
	// before producing an exit status (binary missing or not executable,
	// working directory gone), so a nil Result is not the only signal.
	if ar.err != nil && (ar.result == nil || (ar.result.ExitCode == 0 && !ar.result.TimedOut && !ar.result.Cancelled)) {
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

	// Cancelled (e.g. speculative loser). Mutually exclusive with TimedOut.
	if ar.result.Cancelled {
		return FailureClassSpeculativeCancelled
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

// isAgentFault reports whether a failure indicts the agent itself, as opposed
// to the task, the working directory, or QuanCode's own orchestration. Only
// these count toward the health breaker.
func isAgentFault(ar attemptResult) bool {
	// Setup never reached the agent.
	if ar.orchestrationErr {
		return false
	}
	// The agent binary could not be launched at all (missing, not executable).
	if ar.result == nil {
		return ar.err != nil
	}
	if ar.result.Cancelled || ar.result.TimedOut {
		return false
	}
	if ar.result.ExitCode == 0 {
		// Exit 0 with an error means the process never ran — see
		// classifyFailure. That is the agent's own installation being broken.
		return ar.err != nil
	}
	// Set by agent.applyDiagnosticHints from the shared pattern table.
	return ar.result.AgentFault
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
