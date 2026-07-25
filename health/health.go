// Package health decides which agents automatic routing should currently
// avoid, based on their recent delegation history.
//
// Motivation: a 2026-05..07 ledger review found GitHub Copilot CLI failing
// 66 consecutive delegations over a month while still being chosen as the
// top fallback target (24 of 29 fallback chains) and speculative backup
// (54 of 55 runs). Routing had no idea the agent was dead, so every fallback
// paid the full cost of a guaranteed failure.
//
// Health is *derived from the ledger*, not stored separately. The ledger is
// already append-only, already written by every delegation path, and already
// carries the agent, timestamp, and failure classification. Deriving avoids
// a second source of truth and the read-modify-write races that a shared
// counter file would need locking to survive — two concurrent delegations
// simply append their own lines.
package health

import (
	"fmt"
	"sort"
	"time"

	"github.com/qq418716640/quancode/ledger"
)

// Defaults, overridable via preferences.agent_health.
const (
	DefaultFailureThreshold = 3
	DefaultCooldownSecs     = 900          // 15 minutes
	DefaultMaxCooldownSecs  = 6 * 60 * 60  // 6 hours
	DefaultLookbackSecs     = 24 * 60 * 60 // ignore failures older than a day
	maxBackoffShift         = 12           // ceiling on the exponential shift
	// maxConfigurableSecs (30 days) bounds every duration setting. It keeps
	// the shift in backoff() far away from integer overflow.
	maxConfigurableSecs = 30 * 24 * 60 * 60
)

// Config controls breaker behavior. The zero value means "enabled with
// defaults" — Enabled is a *bool so an explicit `enabled: false` in YAML is
// distinguishable from the field being absent.
type Config struct {
	Enabled          *bool `yaml:"enabled,omitempty"`
	FailureThreshold int   `yaml:"failure_threshold,omitempty"`
	CooldownSecs     int   `yaml:"cooldown_secs,omitempty"`
	MaxCooldownSecs  int   `yaml:"max_cooldown_secs,omitempty"`
	LookbackSecs     int   `yaml:"lookback_secs,omitempty"`
}

// IsEnabled reports whether the breaker is on. Absent config means on.
func (c Config) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

// Normalize fills in defaults and clamps nonsensical values.
func (c Config) Normalize() Config {
	if c.FailureThreshold <= 0 {
		c.FailureThreshold = DefaultFailureThreshold
	}
	if c.CooldownSecs <= 0 {
		c.CooldownSecs = DefaultCooldownSecs
	}
	if c.MaxCooldownSecs <= 0 {
		c.MaxCooldownSecs = DefaultMaxCooldownSecs
	}
	if c.MaxCooldownSecs < c.CooldownSecs {
		c.MaxCooldownSecs = c.CooldownSecs
	}
	if c.LookbackSecs <= 0 {
		c.LookbackSecs = DefaultLookbackSecs
	}
	// Clamp to sane bounds. Besides rejecting nonsense config, this makes the
	// shift in backoff() incapable of overflowing.
	if c.CooldownSecs > maxConfigurableSecs {
		c.CooldownSecs = maxConfigurableSecs
	}
	if c.MaxCooldownSecs > maxConfigurableSecs {
		c.MaxCooldownSecs = maxConfigurableSecs
	}
	if c.LookbackSecs > maxConfigurableSecs {
		c.LookbackSecs = maxConfigurableSecs
	}
	return c
}

// AgentHealth is the derived state of one agent.
type AgentHealth struct {
	Agent string
	// ConsecutiveFaults counts agent-fault failures since the agent's most
	// recent success, within the lookback window.
	ConsecutiveFaults int
	LastFault         time.Time
	LastFaultClass    string
	LastSuccess       time.Time
	// OpenUntil is when the breaker closes again. Zero means closed.
	OpenUntil time.Time
	// SuccessDurationsMs holds the durations of successful runs in the
	// lookback window, used to judge timeout headroom.
	SuccessDurationsMs []int64
}

// SuccessDurationP90 returns the 90th-percentile duration of this agent's
// recent successful runs, in seconds. Returns 0 when there is too little
// history to say anything useful.
func (h *AgentHealth) SuccessDurationP90() int {
	const minSamples = 8
	if h == nil || len(h.SuccessDurationsMs) < minSamples {
		return 0
	}
	d := append([]int64(nil), h.SuccessDurationsMs...)
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	idx := int(float64(len(d)) * 0.9)
	if idx >= len(d) {
		idx = len(d) - 1
	}
	return int(d[idx] / 1000)
}

// Now is the clock used for breaker decisions. Tests override it.
var Now = func() time.Time { return time.Now().UTC() }

// Snapshot is an immutable view of agent health, computed once per command.
// Callers must not hold it across long-running work — build a fresh one per
// delegation attempt if health should reflect the attempt that just ran.
type Snapshot struct {
	cfg    Config
	now    time.Time
	agents map[string]*AgentHealth
}

// NewSnapshot derives health from recent ledger entries. It never fails: an
// unreadable ledger yields an empty snapshot, which reports every agent
// healthy (fail open — health tracking must never block a delegation).
func NewSnapshot(cfg Config) *Snapshot {
	cfg = cfg.Normalize()
	now := Now()
	s := &Snapshot{cfg: cfg, now: now, agents: map[string]*AgentHealth{}}
	if !cfg.IsEnabled() {
		return s
	}

	since := now.Add(-time.Duration(cfg.LookbackSecs) * time.Second)
	entries, err := ledger.ReadSince(since)
	if err != nil {
		return s
	}

	// Oldest first, so the last write for each agent wins.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Timestamp < entries[j].Timestamp
	})

	for _, e := range entries {
		if e.Agent == "" || e.Cancelled {
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			continue
		}
		// A future timestamp means a clock jump; ignore it rather than let it
		// pin an agent open. The small tolerance absorbs sub-second skew
		// between the writing and reading process.
		if ts.After(now.Add(2 * time.Minute)) {
			continue
		}

		h := s.agents[e.Agent]
		if h == nil {
			h = &AgentHealth{Agent: e.Agent}
			s.agents[e.Agent] = h
		}

		switch {
		case isSuccess(e):
			// Any success clears the streak entirely.
			h.ConsecutiveFaults = 0
			h.LastFaultClass = ""
			h.LastSuccess = ts
			if e.DurationMs > 0 {
				h.SuccessDurationsMs = append(h.SuccessDurationsMs, e.DurationMs)
			}
		case indictsAgent(e):
			h.ConsecutiveFaults++
			h.LastFault = ts
			h.LastFaultClass = e.FailureClass
		default:
			// Timeouts, task failures, patch conflicts, orchestration errors:
			// inconclusive. Leave the streak untouched.
		}
	}

	for _, h := range s.agents {
		if h.ConsecutiveFaults >= cfg.FailureThreshold && !h.LastFault.IsZero() {
			h.OpenUntil = h.LastFault.Add(backoff(cfg, h.ConsecutiveFaults))
		}
	}
	return s
}

// backoff grows the cooldown as the failure streak lengthens, so a briefly
// flaky agent recovers fast while a month-dead one is probed rarely. Copilot's
// 66-failure streak reaches the ceiling immediately instead of costing one
// wasted delegation every 15 minutes.
func backoff(cfg Config, faults int) time.Duration {
	shift := faults - cfg.FailureThreshold
	if shift < 0 {
		shift = 0
	}
	if shift > maxBackoffShift {
		shift = maxBackoffShift
	}
	secs := cfg.CooldownSecs << uint(shift)
	if secs > cfg.MaxCooldownSecs || secs <= 0 { // <=0 guards overflow
		secs = cfg.MaxCooldownSecs
	}
	return time.Duration(secs) * time.Second
}

func isSuccess(e ledger.Entry) bool {
	return e.ExitCode == 0 && !e.TimedOut && !e.Cancelled
}

// indictsAgent reports whether a ledger entry blames the agent itself.
//
// The AgentFault field is authoritative for entries written by v0.9.0+.
// Older entries predate it, so rate_limited — which only ever meant an
// upstream/quota rejection — is accepted as an equivalent signal.
func indictsAgent(e ledger.Entry) bool {
	if isSuccess(e) {
		return false
	}
	return e.AgentFault || e.FailureClass == "rate_limited"
}

// IsOpen reports whether automatic routing should skip this agent, with a
// human-readable reason.
//
// Expiry is checked against the live clock rather than the snapshot's
// creation time: a delegation can run for many minutes, and an agent whose
// cooldown elapsed during that run should be usable again for the fallback
// that follows. Newly-failed agents still require a fresh snapshot.
func (s *Snapshot) IsOpen(agent string) (bool, string) {
	if s == nil || !s.cfg.IsEnabled() {
		return false, ""
	}
	h := s.agents[agent]
	if h == nil || h.OpenUntil.IsZero() {
		return false, ""
	}
	now := Now()
	if !now.Before(h.OpenUntil) {
		return false, ""
	}
	remain := h.OpenUntil.Sub(now).Round(time.Second)
	reason := fmt.Sprintf("%d consecutive failures", h.ConsecutiveFaults)
	if h.LastFaultClass != "" {
		reason += " (" + h.LastFaultClass + ")"
	}
	reason += fmt.Sprintf(", skipping for %s", remain)
	return true, reason
}

// Get returns the derived health for an agent, or nil if it has no recent
// history.
func (s *Snapshot) Get(agent string) *AgentHealth {
	if s == nil {
		return nil
	}
	return s.agents[agent]
}

// PreferredProbe returns which of the given agents to force-probe when every
// one of them is unhealthy: the one closest to recovering, then the one that
// succeeded most recently. Returns "" for an empty list.
func (s *Snapshot) PreferredProbe(candidates []string) string {
	if s == nil || len(candidates) == 0 {
		return ""
	}
	best := candidates[0]
	for _, a := range candidates[1:] {
		if s.closerToRecovery(a, best) {
			best = a
		}
	}
	return best
}

// FilterHealthy removes agents whose breaker is open, preserving order.
//
// It never returns an empty list: when every candidate is unhealthy it
// force-probes exactly one — the agent closest to recovering, then the one
// that succeeded most recently. Returning all of them instead would march a
// fallback chain through every known-dead agent in turn.
func (s *Snapshot) FilterHealthy(candidates []string) (healthy []string, forcedProbe string) {
	if s == nil || !s.cfg.IsEnabled() || len(candidates) == 0 {
		return candidates, ""
	}
	for _, a := range candidates {
		if open, _ := s.IsOpen(a); !open {
			healthy = append(healthy, a)
		}
	}
	if len(healthy) > 0 {
		return healthy, ""
	}

	best := s.PreferredProbe(candidates)
	return []string{best}, best
}

// closerToRecovery reports whether agent a is a better forced probe than b.
func (s *Snapshot) closerToRecovery(a, b string) bool {
	ha, hb := s.agents[a], s.agents[b]
	if ha == nil {
		return true // no recent history at all — the safest thing to try
	}
	if hb == nil {
		return false
	}
	if !ha.OpenUntil.Equal(hb.OpenUntil) {
		return ha.OpenUntil.Before(hb.OpenUntil)
	}
	return ha.LastSuccess.After(hb.LastSuccess)
}

// All returns every agent with recent history, for `quancode agents` and the
// dashboard.
func (s *Snapshot) All() map[string]AgentHealth {
	out := map[string]AgentHealth{}
	if s == nil {
		return out
	}
	for k, v := range s.agents {
		out[k] = *v
	}
	return out
}
