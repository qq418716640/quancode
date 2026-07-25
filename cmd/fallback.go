package cmd

import (
	"fmt"
	"os"

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
	// health skips agents whose breaker is open. nil disables the check.
	health router.HealthFilter
	// probeUsed records that the chain already force-probed an unhealthy
	// agent because every candidate was unhealthy. Only one such probe is
	// allowed per chain, so a fallback never marches through every dead
	// agent in turn.
	probeUsed bool
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

// withHealth attaches a health filter so unhealthy agents are skipped.
func (fl *fallbackLoop) withHealth(hf router.HealthFilter) *fallbackLoop {
	fl.health = hf
	return fl
}

// markProbeUsed records that the caller already force-probed an unhealthy
// agent when choosing the initial one, so this chain will not probe a second.
func (fl *fallbackLoop) markProbeUsed() *fallbackLoop {
	fl.probeUsed = true
	return fl
}

// shouldRetry returns true if the failure is transient and attempts remain.
func (fl *fallbackLoop) shouldRetry(ar attemptResult, attempt int) bool {
	return attempt < fl.maxAttempts && isTransientFailure(ar.failureClass)
}

// nextAgent selects the next available fallback agent.
// Returns empty key, nil agent, and empty reason if none available.
func (fl *fallbackLoop) nextAgent() (key string, a agent.Agent, reason string) {
	for {
		sel, probed, skipped := router.SelectHealthy(fl.cfg, fl.task, fl.tried, fl.health, !fl.probeUsed)
		for key, reason := range skipped {
			fmt.Fprintf(os.Stderr, "[quancode] fallback skipping %s — %s\n", key, reason)
		}
		if sel == nil {
			return "", nil, ""
		}
		if probed {
			fl.probeUsed = true
			fmt.Fprintf(os.Stderr, "[quancode] all fallback agents are unhealthy; probing %s anyway\n", sel.AgentKey)
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

// isFallbackEligible returns true if the delegation failure looks transient
// (timeout, launch failure, quota exhaustion, upstream flakiness) rather than
// a legitimate task failure.
//
// Transient detection is driven by config.CommonFailurePatterns, the same
// table that produces diagnostic hints. The agent normally sets
// result.Transient during applyDiagnosticHints; the direct scan below covers
// callers that assemble a Result without going through the agent.
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
	if result.Transient {
		return true
	}
	_, transient := config.MatchFailurePatterns(stdout+"\n"+stderr, nil)
	return transient
}
