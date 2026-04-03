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
		name     string
		ar       attemptResult
		attempt  int
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
