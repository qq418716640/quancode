package config

import (
	"strings"
	"testing"
)

// realFailureOutputs are verbatim excerpts from ~/.config/quancode/logs
// covering the failures observed between 2026-05-06 and 2026-07-25.
// Before v0.9.0 the first two were classified agent_failed and never
// triggered fallback, which accounted for 67 of 204 totally-failed runs.
var realFailureOutputs = []struct {
	name      string
	output    string
	transient bool
	wantHint  bool
}{
	{
		name:      "codex usage limit",
		output:    "ERROR: You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at May 18th, 2026 12:00 AM.",
		transient: true,
		wantHint:  true,
	},
	{
		name:      "copilot transient_bad_request",
		output:    "● Request failed (transient_bad_request). Retrying...\n\n● Request failed (transient_bad_request). Retrying...",
		transient: true,
		wantHint:  true,
	},
	{
		name:      "codex untrusted directory",
		output:    "Reading additional input from stdin...\nNot inside a trusted directory and --skip-git-repo-check was not specified.",
		transient: false,
		wantHint:  true,
	},
	{
		name:      "gemini proxy reset",
		output:    "FetchError: request to https://cloudcode-pa.googleapis.com/ failed, reason: read ECONNRESET",
		transient: true,
		wantHint:  true,
	},
	{
		name:      "ordinary task failure is not transient",
		output:    "FAIL github.com/example/pkg 0.3s\n--- FAIL: TestThing\n    thing_test.go:12: want 3, got 4",
		transient: false,
		wantHint:  false,
	},
	{
		name:      "empty output",
		output:    "",
		transient: false,
		wantHint:  false,
	},
}

func TestMatchFailurePatternsAgainstRealOutput(t *testing.T) {
	for _, tc := range realFailureOutputs {
		t.Run(tc.name, func(t *testing.T) {
			hints, transient := MatchFailurePatterns(tc.output, nil)
			if transient != tc.transient {
				t.Errorf("transient = %v, want %v (output: %.80q)", transient, tc.transient, tc.output)
			}
			if got := len(hints) > 0; got != tc.wantHint {
				t.Errorf("got hints %v, wantHint=%v", hints, tc.wantHint)
			}
		})
	}
}

func TestMatchFailurePatternsIsCaseInsensitive(t *testing.T) {
	// Real CLIs vary the casing; the pre-v0.9.0 per-agent hint scan was
	// case-sensitive and missed these.
	for _, out := range []string{"Rate Limit exceeded", "RATE LIMIT", "rate limit"} {
		if _, transient := MatchFailurePatterns(out, nil); !transient {
			t.Errorf("%q should match case-insensitively", out)
		}
	}
}

func TestMatchFailurePatternsAgentHints(t *testing.T) {
	hints, transient := MatchFailurePatterns("some Custom Failure here", []DiagnosticHint{
		{Pattern: "custom failure", Hint: "do the custom thing", Transient: true},
	})
	if !transient {
		t.Error("agent hint with Transient=true should mark the failure transient")
	}
	if len(hints) != 1 || hints[0] != "do the custom thing" {
		t.Errorf("got %v, want [do the custom thing]", hints)
	}
}

func TestMatchFailurePatternsNonTransientAgentHint(t *testing.T) {
	hints, transient := MatchFailurePatterns("Execution error", []DiagnosticHint{
		{Pattern: "Execution error", Hint: "retry once"},
	})
	if transient {
		t.Error("agent hint without Transient should not trigger fallback")
	}
	if len(hints) != 1 {
		t.Errorf("hint should still be reported, got %v", hints)
	}
}

func TestMatchFailurePatternsDeduplicatesHints(t *testing.T) {
	// "rate limit" and "rate_limit" carry the same hint text; a message
	// containing both must not print it twice.
	hints, _ := MatchFailurePatterns("rate limit hit, rate_limit hit", nil)
	seen := map[string]int{}
	for _, h := range hints {
		seen[h]++
		if seen[h] > 1 {
			t.Errorf("hint duplicated: %q", h)
		}
	}
}

func TestCommonFailurePatternsWellFormed(t *testing.T) {
	for i, p := range CommonFailurePatterns {
		if p.Pattern == "" {
			t.Errorf("CommonFailurePatterns[%d] has empty Pattern", i)
		}
		if p.Hint == "" {
			t.Errorf("CommonFailurePatterns[%d] (%q) has no Hint; every pattern should explain itself", i, p.Pattern)
		}
		if p.Pattern != strings.ToLower(p.Pattern) {
			t.Errorf("CommonFailurePatterns[%d] (%q) should be lowercase for readability", i, p.Pattern)
		}
	}
}
