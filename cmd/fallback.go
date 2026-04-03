package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/config"
	"github.com/qq418716640/quancode/router"
	"github.com/qq418716640/quancode/runner"
)

const defaultMaxFallbackAttempts = 3

// fallbackLoop tracks tried agents and selects fallbacks on transient failures.
type fallbackLoop struct {
	cfg         *config.Config
	task        string
	isolation   string // required isolation mode; agents that don't support it are skipped
	tried       map[string]bool
	maxAttempts int
}

// newFallbackLoop creates a fallback loop with the initial agent already marked as tried.
// isolation is the required isolation mode for fallback agents (empty means no filtering).
func newFallbackLoop(cfg *config.Config, task, initialAgent, isolation string, maxAttempts int) *fallbackLoop {
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxFallbackAttempts
	}
	return &fallbackLoop{
		cfg:         cfg,
		task:        task,
		isolation:   isolation,
		tried:       map[string]bool{initialAgent: true},
		maxAttempts: maxAttempts,
	}
}

// shouldRetry returns true if the failure is transient and attempts remain.
func (fl *fallbackLoop) shouldRetry(ar attemptResult, attempt int) bool {
	return attempt < fl.maxAttempts && isTransientFailure(ar.failureClass)
}

// nextAgent selects the next available fallback agent.
// Returns empty key, nil agent, and empty reason if none available.
func (fl *fallbackLoop) nextAgent() (key string, a agent.Agent, reason string) {
	for {
		sel := router.SelectAgentExcluding(fl.cfg, fl.task, fl.tried)
		if sel == nil {
			return "", nil, ""
		}
		fl.tried[sel.AgentKey] = true

		ac := fl.cfg.Agents[sel.AgentKey]
		// Skip agents that don't support the required isolation mode.
		if fl.isolation != "" && !ac.SupportsIsolation(fl.isolation) {
			fmt.Fprintf(os.Stderr, "[quancode] fallback %s does not support isolation %s, skipping\n", sel.AgentKey, fl.isolation)
			continue
		}
		next := agent.FromConfig(sel.AgentKey, ac)
		if ok, _ := next.IsAvailable(); !ok {
			fmt.Fprintf(os.Stderr, "[quancode] fallback %s not available, skipping\n", sel.AgentKey)
			continue
		}
		return sel.AgentKey, next, sel.Reason
	}
}

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
