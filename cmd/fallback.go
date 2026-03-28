package cmd

import (
	"strings"

	"github.com/qq418716640/quancode/runner"
)

// rateLimitPatterns are stderr substrings that indicate a transient rate-limit
// or capacity error, where retrying with a different agent may succeed.
var rateLimitPatterns = []string{
	"rate limit",
	"rate_limit",
	"too many requests",
	"quota exceeded",
	"try again later",
	"overloaded",
	"service unavailable",
	"capacity",
	"throttled",
	"429",
}

// isFallbackEligible returns true if the delegation failure looks transient
// (timeout or rate-limit) rather than a legitimate task failure.
func isFallbackEligible(result *runner.Result, output string) bool {
	if result == nil {
		return false
	}
	if result.TimedOut {
		return true
	}
	if result.ExitCode == 0 {
		return false
	}
	// Check output for rate-limit patterns
	lower := strings.ToLower(output)
	for _, pattern := range rateLimitPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}
