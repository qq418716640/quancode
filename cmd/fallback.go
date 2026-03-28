package cmd

import (
	"strings"

	"github.com/qq418716640/quancode/runner"
)

// rateLimitPatterns are stderr/stdout substrings that indicate a transient
// rate-limit or capacity error, where retrying with a different agent may succeed.
var rateLimitPatterns = []string{
	"rate limit",
	"rate_limit",
	"too many requests",
	"quota exceeded",
	"try again later",
	"overloaded",
	"service unavailable",
	"throttled",
}

// isFallbackEligible returns true if the delegation failure looks transient
// (timeout, launch failure, or rate-limit) rather than a legitimate task failure.
func isFallbackEligible(result *runner.Result, stdout, stderr string) bool {
	// Launch failure (couldn't start the agent at all)
	if result == nil {
		return true
	}
	if result.TimedOut {
		return true
	}
	if result.ExitCode == 0 {
		return false
	}
	// Check both stdout and stderr for rate-limit patterns
	combined := strings.ToLower(stdout + " " + stderr)
	for _, pattern := range rateLimitPatterns {
		if strings.Contains(combined, pattern) {
			return true
		}
	}
	return false
}
