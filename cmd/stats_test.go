package cmd

import (
	"strings"
	"testing"

	"github.com/qq418716640/quancode/ledger"
)

func TestStatsIncludesFallbackAnalysis(t *testing.T) {
	isolateHome(t)

	runID := "run_test123"

	// First attempt: alpha timed out
	if err := ledger.Append(&ledger.Entry{
		Agent:      "alpha",
		Task:       "fix bug",
		WorkDir:    "/tmp/repo",
		ExitCode:   124,
		TimedOut:   true,
		DurationMs: 5000,
		RunID:      runID,
		Attempt:    1,
	}); err != nil {
		t.Fatalf("Append entry 1: %v", err)
	}
	// Second attempt: beta succeeded after fallback
	if err := ledger.Append(&ledger.Entry{
		Agent:          "beta",
		Task:           "fix bug",
		WorkDir:        "/tmp/repo",
		ExitCode:       0,
		DurationMs:     3000,
		RunID:          runID,
		Attempt:        2,
		FallbackFrom:   "alpha",
		FallbackReason: "timed_out",
	}); err != nil {
		t.Fatalf("Append entry 2: %v", err)
	}
	// A separate single-attempt run (no fallback)
	if err := ledger.Append(&ledger.Entry{
		Agent:      "alpha",
		Task:       "write tests",
		WorkDir:    "/tmp/repo",
		ExitCode:   0,
		DurationMs: 2000,
		RunID:      "run_other456",
		Attempt:    1,
	}); err != nil {
		t.Fatalf("Append entry 3: %v", err)
	}

	oldDays := statsDays
	statsDays = 30
	defer func() { statsDays = oldDays }()

	out := captureStdout(t, func() {
		if err := statsCmd.RunE(statsCmd, nil); err != nil {
			t.Fatalf("stats RunE: %v", err)
		}
	})

	for _, want := range []string{
		"fallback analysis",
		"1/2 runs triggered fallback",
		"100% recovered",
		"timed_out=1",
		"alpha → beta",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected stats output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestStatsNoFallbackSectionWhenNoRunTracking(t *testing.T) {
	isolateHome(t)

	// Old-style entry without RunID
	if err := ledger.Append(&ledger.Entry{
		Agent:      "codex",
		Task:       "task",
		WorkDir:    "/tmp/repo",
		ExitCode:   0,
		DurationMs: 100,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	oldDays := statsDays
	statsDays = 30
	defer func() { statsDays = oldDays }()

	out := captureStdout(t, func() {
		if err := statsCmd.RunE(statsCmd, nil); err != nil {
			t.Fatalf("stats RunE: %v", err)
		}
	})

	if strings.Contains(out, "fallback analysis") {
		t.Fatalf("expected no fallback section for old data, got:\n%s", out)
	}
}

