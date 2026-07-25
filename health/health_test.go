package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qq418716640/quancode/ledger"
)

// withLedger points the ledger at a temp dir seeded with the given entries
// and pins the clock, restoring both afterwards.
func withLedger(t *testing.T, now time.Time, entries []ledger.Entry) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	logDir := filepath.Join(dir, ".config", "quancode", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}
	byDay := map[string][]byte{}
	for _, e := range entries {
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			t.Fatalf("bad timestamp %q: %v", e.Timestamp, err)
		}
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		day := ts.Format("2006-01-02")
		byDay[day] = append(byDay[day], append(data, '\n')...)
	}
	for day, data := range byDay {
		if err := os.WriteFile(filepath.Join(logDir, day+".jsonl"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}

	old := Now
	Now = func() time.Time { return now }
	t.Cleanup(func() { Now = old })
}

func fault(agent string, at time.Time) ledger.Entry {
	return ledger.Entry{
		Agent:        agent,
		Timestamp:    at.Format(time.RFC3339),
		ExitCode:     1,
		FailureClass: "rate_limited",
		AgentFault:   true,
	}
}

func success(agent string, at time.Time) ledger.Entry {
	return ledger.Entry{Agent: agent, Timestamp: at.Format(time.RFC3339), ExitCode: 0}
}

func TestBreakerOpensAtThreshold(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	withLedger(t, now, []ledger.Entry{
		fault("copilot", now.Add(-3*time.Minute)),
		fault("copilot", now.Add(-2*time.Minute)),
		fault("copilot", now.Add(-1*time.Minute)),
	})

	s := NewSnapshot(Config{})
	open, reason := s.IsOpen("copilot")
	if !open {
		t.Fatal("breaker should be open after 3 consecutive agent faults")
	}
	if reason == "" {
		t.Error("expected a human-readable reason")
	}
}

func TestBreakerStaysClosedBelowThreshold(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	withLedger(t, now, []ledger.Entry{
		fault("codex", now.Add(-2*time.Minute)),
		fault("codex", now.Add(-1*time.Minute)),
	})

	if open, _ := NewSnapshot(Config{}).IsOpen("codex"); open {
		t.Error("2 failures should not open the breaker (threshold is 3)")
	}
}

func TestSuccessResetsStreak(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	withLedger(t, now, []ledger.Entry{
		fault("codex", now.Add(-5*time.Minute)),
		fault("codex", now.Add(-4*time.Minute)),
		success("codex", now.Add(-3*time.Minute)),
		fault("codex", now.Add(-2*time.Minute)),
	})

	if open, _ := NewSnapshot(Config{}).IsOpen("codex"); open {
		t.Error("a success in the middle should reset the streak")
	}
}

// TestTimeoutsDoNotOpenBreaker is the guard against the review's key risk:
// codex timed out 128 times in the window while succeeding 82% overall.
// Timeouts track task difficulty, not agent health.
func TestTimeoutsDoNotOpenBreaker(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	var entries []ledger.Entry
	for i := 1; i <= 6; i++ {
		entries = append(entries, ledger.Entry{
			Agent:        "codex",
			Timestamp:    now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			ExitCode:     1,
			TimedOut:     true,
			FailureClass: "timed_out",
		})
	}
	withLedger(t, now, entries)

	if open, _ := NewSnapshot(Config{}).IsOpen("codex"); open {
		t.Error("timeouts must not open the breaker")
	}
}

// TestTaskFailuresDoNotOpenBreaker: a bad task must never disable an agent.
func TestTaskFailuresDoNotOpenBreaker(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	var entries []ledger.Entry
	for i := 1; i <= 6; i++ {
		entries = append(entries, ledger.Entry{
			Agent:        "codex",
			Timestamp:    now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			ExitCode:     1,
			FailureClass: "agent_failed",
		})
	}
	withLedger(t, now, entries)

	if open, _ := NewSnapshot(Config{}).IsOpen("codex"); open {
		t.Error("plain task failures must not open the breaker")
	}
}

// TestOrchestrationErrorsDoNotOpenBreaker: worktree/git problems are ours.
func TestOrchestrationErrorsDoNotOpenBreaker(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	var entries []ledger.Entry
	for i := 1; i <= 6; i++ {
		entries = append(entries, ledger.Entry{
			Agent:        "codex",
			Timestamp:    now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			ExitCode:     1,
			FailureClass: "orchestration_error",
		})
	}
	withLedger(t, now, entries)

	if open, _ := NewSnapshot(Config{}).IsOpen("codex"); open {
		t.Error("orchestration errors must not be blamed on the agent")
	}
}

func TestCooldownExpiryReopensAgent(t *testing.T) {
	base := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	entries := []ledger.Entry{
		fault("copilot", base.Add(-2*time.Minute)),
		fault("copilot", base.Add(-1*time.Minute)),
		fault("copilot", base),
	}

	// Immediately after the third failure: open.
	withLedger(t, base.Add(time.Minute), entries)
	if open, _ := NewSnapshot(Config{}).IsOpen("copilot"); !open {
		t.Fatal("should be open one minute after tripping")
	}

	// After the 15-minute cooldown: half-open, allowed through.
	withLedger(t, base.Add(16*time.Minute), entries)
	if open, _ := NewSnapshot(Config{}).IsOpen("copilot"); open {
		t.Error("should be closed once the cooldown expires")
	}
}

// TestBackoffGrowsWithStreak replays copilot's real behaviour: 66 straight
// failures should back off to the ceiling, not keep probing every 15 minutes.
func TestBackoffGrowsWithStreak(t *testing.T) {
	base := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	var entries []ledger.Entry
	for i := 66; i >= 1; i-- {
		entries = append(entries, fault("copilot", base.Add(-time.Duration(i)*time.Minute)))
	}

	// Five hours later the ceiling (6h) should still hold it open.
	withLedger(t, base.Add(5*time.Hour), entries)
	if open, _ := NewSnapshot(Config{}).IsOpen("copilot"); !open {
		t.Error("a 66-failure streak should still be open after 5 hours")
	}

	// Past the 6h ceiling it must reopen — the breaker is never permanent.
	withLedger(t, base.Add(7*time.Hour), entries)
	if open, _ := NewSnapshot(Config{}).IsOpen("copilot"); open {
		t.Error("breaker must reopen past the max cooldown")
	}
}

func TestLookbackWindowIgnoresAncientFailures(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	old := now.Add(-72 * time.Hour)
	withLedger(t, now, []ledger.Entry{
		fault("codex", old),
		fault("codex", old.Add(time.Minute)),
		fault("codex", old.Add(2*time.Minute)),
	})

	if open, _ := NewSnapshot(Config{}).IsOpen("codex"); open {
		t.Error("failures older than the lookback window must not count")
	}
}

func TestDisabledConfigNeverOpens(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	withLedger(t, now, []ledger.Entry{
		fault("copilot", now.Add(-3*time.Minute)),
		fault("copilot", now.Add(-2*time.Minute)),
		fault("copilot", now.Add(-1*time.Minute)),
	})

	off := false
	if open, _ := NewSnapshot(Config{Enabled: &off}).IsOpen("copilot"); open {
		t.Error("an explicitly disabled breaker must never open")
	}
}

func TestFilterHealthyForcesSingleProbeWhenAllUnhealthy(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	var entries []ledger.Entry
	// copilot: long-dead. gemini: just tripped, so closer to recovering.
	for i := 20; i >= 1; i-- {
		entries = append(entries, fault("copilot", now.Add(-time.Duration(i)*time.Minute)))
	}
	for i := 3; i >= 1; i-- {
		entries = append(entries, fault("gemini", now.Add(-time.Duration(i)*time.Second)))
	}
	withLedger(t, now, entries)

	s := NewSnapshot(Config{})
	got, forced := s.FilterHealthy([]string{"copilot", "gemini"})
	if len(got) != 1 {
		t.Fatalf("expected exactly one forced probe, got %v", got)
	}
	if forced != "gemini" {
		t.Errorf("forced probe = %q, want gemini (closest to recovery)", forced)
	}
}

func TestFilterHealthyKeepsHealthyAgents(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	withLedger(t, now, []ledger.Entry{
		fault("copilot", now.Add(-3*time.Minute)),
		fault("copilot", now.Add(-2*time.Minute)),
		fault("copilot", now.Add(-1*time.Minute)),
		success("codex", now.Add(-time.Minute)),
	})

	got, forced := NewSnapshot(Config{}).FilterHealthy([]string{"copilot", "codex"})
	if len(got) != 1 || got[0] != "codex" {
		t.Errorf("FilterHealthy = %v, want [codex]", got)
	}
	if forced != "" {
		t.Errorf("no probe should be forced when a healthy agent exists, got %q", forced)
	}
}

func TestUnknownAgentIsHealthy(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	withLedger(t, now, nil)
	if open, _ := NewSnapshot(Config{}).IsOpen("never-seen"); open {
		t.Error("an agent with no history must read as healthy")
	}
}

func TestFutureTimestampsIgnored(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	withLedger(t, now, []ledger.Entry{
		fault("codex", now.Add(48*time.Hour)),
		fault("codex", now.Add(49*time.Hour)),
		fault("codex", now.Add(50*time.Hour)),
	})

	if open, _ := NewSnapshot(Config{}).IsOpen("codex"); open {
		t.Error("clock-skewed future entries must not pin the breaker open")
	}
}

func TestLegacyRateLimitedEntriesCount(t *testing.T) {
	// Entries written before v0.9.0 have no agent_fault field.
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	var entries []ledger.Entry
	for i := 3; i >= 1; i-- {
		entries = append(entries, ledger.Entry{
			Agent:        "copilot",
			Timestamp:    now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			ExitCode:     1,
			FailureClass: "rate_limited",
		})
	}
	withLedger(t, now, entries)

	if open, _ := NewSnapshot(Config{}).IsOpen("copilot"); !open {
		t.Error("legacy rate_limited entries should still count toward health")
	}
}

func TestCancelledEntriesIgnored(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	var entries []ledger.Entry
	for i := 5; i >= 1; i-- {
		entries = append(entries, ledger.Entry{
			Agent:        "codex",
			Timestamp:    now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			ExitCode:     1,
			Cancelled:    true,
			AgentFault:   true,
			FailureClass: "speculative_cancelled",
		})
	}
	withLedger(t, now, entries)

	if open, _ := NewSnapshot(Config{}).IsOpen("codex"); open {
		t.Error("speculative losers we cancelled ourselves must not count")
	}
}

func TestConfigNormalizeClampsBadValues(t *testing.T) {
	c := Config{FailureThreshold: -5, CooldownSecs: -1, MaxCooldownSecs: 10, LookbackSecs: 0}.Normalize()
	if c.FailureThreshold != DefaultFailureThreshold {
		t.Errorf("FailureThreshold = %d, want %d", c.FailureThreshold, DefaultFailureThreshold)
	}
	if c.CooldownSecs != DefaultCooldownSecs {
		t.Errorf("CooldownSecs = %d, want %d", c.CooldownSecs, DefaultCooldownSecs)
	}
	if c.MaxCooldownSecs < c.CooldownSecs {
		t.Errorf("MaxCooldownSecs %d must not be below CooldownSecs %d", c.MaxCooldownSecs, c.CooldownSecs)
	}
	if c.LookbackSecs != DefaultLookbackSecs {
		t.Errorf("LookbackSecs = %d, want %d", c.LookbackSecs, DefaultLookbackSecs)
	}
}

func TestAbsentConfigMeansEnabled(t *testing.T) {
	if !(Config{}).IsEnabled() {
		t.Error("absent agent_health config should mean enabled")
	}
	off := false
	if (Config{Enabled: &off}).IsEnabled() {
		t.Error("enabled:false must disable")
	}
	on := true
	if !(Config{Enabled: &on}).IsEnabled() {
		t.Error("enabled:true must enable")
	}
}

func TestSuccessDurationP90(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	var entries []ledger.Entry
	// 10 successes: nine fast, one slow. p90 should land on the slow tail.
	durations := []int64{100_000, 110_000, 120_000, 130_000, 140_000, 150_000, 160_000, 170_000, 180_000, 470_000}
	for i, d := range durations {
		entries = append(entries, ledger.Entry{
			Agent:      "codex",
			Timestamp:  now.Add(-time.Duration(i+1) * time.Minute).Format(time.RFC3339),
			ExitCode:   0,
			DurationMs: d,
		})
	}
	withLedger(t, now, entries)

	p90 := NewSnapshot(Config{}).Get("codex").SuccessDurationP90()
	if p90 < 180 {
		t.Errorf("p90 = %ds, expected the slow tail to show through (>=180s)", p90)
	}
}

func TestSuccessDurationP90NeedsEnoughSamples(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	withLedger(t, now, []ledger.Entry{
		{Agent: "codex", Timestamp: now.Add(-time.Minute).Format(time.RFC3339), ExitCode: 0, DurationMs: 400_000},
	})
	if p90 := NewSnapshot(Config{}).Get("codex").SuccessDurationP90(); p90 != 0 {
		t.Errorf("p90 = %d, want 0 — one sample is not enough to advise on timeouts", p90)
	}
}

func TestSuccessDurationP90NilAgent(t *testing.T) {
	var h *AgentHealth
	if got := h.SuccessDurationP90(); got != 0 {
		t.Errorf("nil AgentHealth p90 = %d, want 0", got)
	}
}
