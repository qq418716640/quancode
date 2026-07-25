package cmd

import (
	"testing"

	"github.com/qq418716640/quancode/config"
	"github.com/qq418716640/quancode/runner"
)

func TestFallbackLoop_ShouldRetry(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"a": {Command: "echo", Enabled: true, Priority: 10},
		},
	}
	fl := newFallbackLoop(cfg, "test task", "a", "", 3)

	tests := []struct {
		name      string
		ar        attemptResult
		attempt   int
		wantRetry bool
	}{
		{
			name:      "transient timeout retries",
			ar:        attemptResult{result: &runner.Result{TimedOut: true}, failureClass: FailureClassTimedOut},
			attempt:   1,
			wantRetry: true,
		},
		{
			name:      "launch failure retries",
			ar:        attemptResult{failureClass: FailureClassLaunchFailure},
			attempt:   1,
			wantRetry: true,
		},
		{
			name:      "rate limited retries",
			ar:        attemptResult{result: &runner.Result{ExitCode: 1}, failureClass: FailureClassRateLimited},
			attempt:   2,
			wantRetry: true,
		},
		{
			name:      "agent failed does not retry",
			ar:        attemptResult{result: &runner.Result{ExitCode: 1}, failureClass: FailureClassAgentFailed},
			attempt:   1,
			wantRetry: false,
		},
		{
			name:      "success does not retry",
			ar:        attemptResult{result: &runner.Result{ExitCode: 0}},
			attempt:   1,
			wantRetry: false,
		},
		{
			name:      "max attempts exceeded",
			ar:        attemptResult{result: &runner.Result{TimedOut: true}, failureClass: FailureClassTimedOut},
			attempt:   3,
			wantRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fl.shouldRetry(tt.ar, tt.attempt)
			if got != tt.wantRetry {
				t.Errorf("shouldRetry = %v, want %v", got, tt.wantRetry)
			}
		})
	}
}

// TestRealWorldQuotaFailuresTriggerFallback is the regression test for the
// 2026-07 log review: 105 attempts (40 codex quota + 65 copilot upstream)
// were classified agent_failed and silently skipped fallback because their
// wording matched none of the old rateLimitPatterns entries.
func TestRealWorldQuotaFailuresTriggerFallback(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
	}{
		{
			name:   "codex usage limit",
			stdout: "ERROR: You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at Jul 29th, 2026 10:11 PM.",
		},
		{
			name:   "copilot transient bad request",
			stdout: "● Request failed (transient_bad_request). Retrying...",
		},
	}

	cfg := &config.Config{Agents: map[string]config.AgentConfig{"a": {Command: "echo", Enabled: true}}}
	fl := newFallbackLoop(cfg, "task", "a", "", 3)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := &runner.Result{ExitCode: 1, Stdout: tc.stdout}

			if !isFallbackEligible(result, tc.stdout, "") {
				t.Fatal("failure should be fallback-eligible")
			}

			ar := attemptResult{result: result, output: tc.stdout}
			ar.failureClass = classifyFailure(ar)
			if ar.failureClass != FailureClassRateLimited {
				t.Errorf("failureClass = %q, want %q", ar.failureClass, FailureClassRateLimited)
			}
			if !fl.shouldRetry(ar, 1) {
				t.Error("shouldRetry = false, want true — this is the bug that lost 67 runs")
			}
		})
	}
}

// TestGenuineTaskFailureDoesNotFallback guards the other direction: widening
// the transient table must not turn ordinary failures into quota burn.
func TestGenuineTaskFailureDoesNotFallback(t *testing.T) {
	stdout := "--- FAIL: TestParseConfig\n    config_test.go:88: unexpected nil"
	result := &runner.Result{ExitCode: 1, Stdout: stdout}

	if isFallbackEligible(result, stdout, "") {
		t.Error("a real test failure must not be fallback-eligible")
	}
	ar := attemptResult{result: result, output: stdout}
	if got := classifyFailure(ar); got != FailureClassAgentFailed {
		t.Errorf("failureClass = %q, want %q", got, FailureClassAgentFailed)
	}
}

// TestResultTransientFlagHonored verifies the agent-set flag path (the agent
// scans per-agent DiagnosticHints that cmd cannot see).
func TestResultTransientFlagHonored(t *testing.T) {
	result := &runner.Result{ExitCode: 1, Transient: true, Stdout: "some cli-specific wording"}
	if !isFallbackEligible(result, result.Stdout, "") {
		t.Error("result.Transient set by the agent should make the failure eligible")
	}
}

func TestFallbackLoop_NextAgent(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"primary":  {Command: "echo", Enabled: true, Priority: 10},
			"backup":   {Command: "echo", Enabled: true, Priority: 20},
			"disabled": {Command: "echo", Enabled: false, Priority: 30},
		},
	}

	fl := newFallbackLoop(cfg, "test", "primary", "", 3)

	// First call should return backup (primary is already tried)
	key, a, reason := fl.nextAgent()
	if key != "backup" {
		t.Errorf("first nextAgent = %q, want %q", key, "backup")
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}

	// Second call should return nothing (backup tried, disabled is disabled)
	key2, a2, _ := fl.nextAgent()
	if key2 != "" || a2 != nil {
		t.Errorf("second nextAgent = %q, want empty", key2)
	}
}

func TestFallbackLoop_DefaultMaxAttempts(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"a": {Command: "echo", Enabled: true},
		},
	}
	fl := newFallbackLoop(cfg, "task", "a", "", 0) // 0 = default
	if fl.maxAttempts != defaultMaxFallbackAttempts {
		t.Errorf("maxAttempts = %d, want %d", fl.maxAttempts, defaultMaxFallbackAttempts)
	}
}
