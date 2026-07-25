package config

import "strings"

// FailurePattern describes a known agent failure signature: a substring
// matched against the agent's stdout+stderr, an actionable recovery hint,
// and whether the failure is transient (worth retrying on another agent).
//
// One table drives both the fallback decision and the diagnostic hint so
// the two cannot drift apart. Before v0.9.0 they were separate lists and
// both went stale: the fallback list never learned the phrasing real CLIs
// use for quota exhaustion, so rate-limited failures were classified
// agent_failed and never triggered fallback.
type FailurePattern struct {
	Pattern string
	Hint    string
	// Transient marks a failure worth immediately retrying on another agent.
	Transient bool
	// AgentFault marks a failure that indicts the agent itself rather than
	// the task or the working directory. Only these count toward the health
	// breaker, so a bad task or a non-git workdir can never disable an agent
	// that is working fine.
	AgentFault bool
}

// CommonFailurePatterns are scanned against every agent's failure output
// regardless of which CLI produced it. Per-agent AgentConfig.DiagnosticHints
// are scanned in addition to these.
//
// Matching is case-insensitive substring matching. Patterns are recorded
// verbatim from observed CLI output — when adding one, paste the real
// message rather than guessing the phrasing.
var CommonFailurePatterns = []FailurePattern{
	// --- Quota / rate limit: transient, another agent usually succeeds ---
	{
		Pattern:    "usage limit",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent hit its account usage limit. Falling back to another agent; check the provider's usage page to confirm when quota resets.",
	},
	{
		Pattern:    "rate limit",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent is rate limited. Falling back to another agent.",
	},
	{
		Pattern:    "rate_limit",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent is rate limited. Falling back to another agent.",
	},
	{
		Pattern:    "too many requests",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent returned HTTP 429. Falling back to another agent.",
	},
	{
		Pattern:    "quota exceeded",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent quota exceeded. Falling back to another agent.",
	},
	{
		Pattern:    "insufficient_quota",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent quota exhausted. Falling back to another agent.",
	},
	{
		Pattern:    "try again later",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent asked to retry later. Falling back to another agent.",
	},
	{
		// Codex phrases quota exhaustion as "...or try again at Jul 29th, 2026
		// 10:11 PM." — "try again later" does not match it.
		Pattern:    "try again at",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent asked to retry at a later time. Falling back to another agent.",
	},
	{
		Pattern:    "throttled",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent is throttled. Falling back to another agent.",
	},

	// --- Upstream/transport flakiness: transient ---
	{
		// GitHub Copilot CLI emits "Request failed (transient_bad_request).
		// Retrying..." and then exits non-zero once its own retries run out.
		Pattern:    "transient_bad_request",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent's upstream rejected the request repeatedly (transient_bad_request). Falling back to another agent.",
	},
	{
		Pattern:    "overloaded",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent's upstream is overloaded. Falling back to another agent.",
	},
	{
		Pattern:    "service unavailable",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent's upstream returned 503. Falling back to another agent.",
	},
	{
		Pattern:    "temporarily unavailable",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent's upstream is temporarily unavailable. Falling back to another agent.",
	},
	{
		Pattern:    "bad gateway",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent's upstream returned 502. Falling back to another agent.",
	},
	{
		Pattern:    "gateway timeout",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent's upstream returned 504. Falling back to another agent.",
	},
	{
		Pattern:    "econnreset",
		Transient:  true,
		AgentFault: true,
		Hint:       "Connection reset while talking to the agent's upstream. If this repeats, check the proxy configured in the agent's env block.",
	},
	{
		// Codex: "ERROR: stream disconnected before completion: error sending
		// request for url (...)" after exhausting its own reconnect attempts.
		Pattern:    "stream disconnected",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent's response stream dropped before completion. Falling back to another agent.",
	},
	{
		// Codex: "ERROR: Selected model is at capacity. Please try a different model."
		Pattern:    "at capacity",
		Transient:  true,
		AgentFault: true,
		Hint:       "The selected model is at capacity. Falling back to another agent.",
	},

	// --- Agent unusable until a human fixes it ---
	// Transient because a different agent can do the job right now; AgentFault
	// because the breaker should stop re-selecting this one after a few tries.
	{
		Pattern:    "not logged in",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent login expired. Re-authenticate with that CLI's login command.",
	},
	{
		Pattern:    "authentication failed",
		Transient:  true,
		AgentFault: true,
		Hint:       "Agent authentication failed. Re-authenticate with that CLI's login command.",
	},
	{
		// Codex: "unexpected status 404 Not Found: Model not found gpt-5.4".
		Pattern:    "model not found",
		Transient:  true,
		AgentFault: true,
		Hint:       "The agent requested a model its provider does not recognize. Check the model name in that agent's primary_args/delegate_args or its own CLI config.",
	},

	// --- Task or workdir problems: neither transient nor the agent's fault ---
	// Retrying elsewhere just repeats the same failure, and counting these
	// toward health would let one bad task or a non-git directory disable a
	// perfectly healthy agent.
	{
		Pattern: "not inside a trusted directory",
		Hint:    "Codex refused to run: the working directory is not a trusted git repo. Run `git init`, or add --skip-git-repo-check to the agent's delegate_args.",
	},
	{
		Pattern: "context length",
		Hint:    "Task plus injected context exceeded the model's context window. Reduce --context-files, drop --context-diff, or split the task.",
	},
	{
		Pattern: "context_length_exceeded",
		Hint:    "Task plus injected context exceeded the model's context window. Reduce --context-files, drop --context-diff, or split the task.",
	},
}

// FailureMatch is the result of scanning an agent's failure output.
type FailureMatch struct {
	// Hints are the recovery messages for every pattern that matched.
	Hints []string
	// Transient is true when any match warrants an immediate retry on a
	// different agent.
	Transient bool
	// AgentFault is true when any match indicts the agent itself, which is
	// what the health breaker counts.
	AgentFault bool
}

// MatchFailurePatterns scans output against CommonFailurePatterns plus the
// supplied per-agent hints.
//
// Matching is case-insensitive. Duplicate hint messages are emitted once.
func MatchFailurePatterns(output string, agentHints []DiagnosticHint) (hints []string, transient bool) {
	m := MatchFailure(output, agentHints)
	return m.Hints, m.Transient
}

// MatchFailure is the full-detail form of MatchFailurePatterns.
func MatchFailure(output string, agentHints []DiagnosticHint) FailureMatch {
	var m FailureMatch
	if output == "" {
		return m
	}
	lower := strings.ToLower(output)
	seen := make(map[string]bool)

	add := func(p FailurePattern) {
		if p.Pattern == "" {
			return
		}
		if !strings.Contains(lower, strings.ToLower(p.Pattern)) {
			return
		}
		if p.Transient {
			m.Transient = true
		}
		if p.AgentFault {
			m.AgentFault = true
		}
		if p.Hint != "" && !seen[p.Hint] {
			seen[p.Hint] = true
			m.Hints = append(m.Hints, p.Hint)
		}
	}

	for _, p := range CommonFailurePatterns {
		add(p)
	}
	// Per-agent hints run second so a CLI-specific message appears after the
	// generic one when both match.
	for _, h := range agentHints {
		add(FailurePattern{Pattern: h.Pattern, Hint: h.Hint, Transient: h.Transient, AgentFault: h.Transient})
	}
	return m
}
