package config

import (
	"slices"
	"strings"
	"testing"
)

// Captured verbatim from `codex exec --full-auto --ephemeral --json` on
// codex-cli 0.145.0. Note the exit code was 0 and stdout was clean JSONL —
// this line, on stderr, was the only sign anything was wrong.
const codexDeprecationStderr = "warning: `--full-auto` is deprecated; use `--sandbox workspace-write` instead.\n" +
	"Reading additional input from stdin...\n"

func TestMatchDeprecationsRealCodexWarning(t *testing.T) {
	got := MatchDeprecations(codexDeprecationStderr)

	want := []string{"warning: `--full-auto` is deprecated; use `--sandbox workspace-write` instead."}
	if !slices.Equal(got, want) {
		t.Errorf("MatchDeprecations() = %q, want %q", got, want)
	}
}

// The stem, not the exact word: Python-based CLIs say "DeprecationWarning",
// which does not contain "deprecated".
func TestMatchDeprecationsCoversWordForms(t *testing.T) {
	forms := []string{
		"warning: --foo is deprecated",
		"--foo has been deprecated and will be removed in 2.0",
		"DeprecationWarning: 'bar' is obsolete",
		"note: deprecating --baz next release",
	}
	for _, line := range forms {
		if got := MatchDeprecations(line); len(got) != 1 {
			t.Errorf("MatchDeprecations(%q) = %q, want one match", line, got)
		}
	}
}

// Only the matched line is reported, never the surrounding buffer: QuanCode
// cannot prove the line came from the CLI's own argument parsing, so the
// reader gets exactly what was said and nothing else.
func TestMatchDeprecationsReportsOnlyMatchedLines(t *testing.T) {
	stderr := "loading plugins\n" +
		"warning: --old is deprecated\n" +
		"connected to upstream\n" +
		"done in 4.2s\n"
	got := MatchDeprecations(stderr)

	if len(got) != 1 || got[0] != "warning: --old is deprecated" {
		t.Errorf("MatchDeprecations() = %q, want just the matched line", got)
	}
}

func TestMatchDeprecationsIgnoresUnrelatedOutput(t *testing.T) {
	for _, s := range []string{"", "all fine\n", "error: connection reset\nretrying\n"} {
		if got := MatchDeprecations(s); got != nil {
			t.Errorf("MatchDeprecations(%q) = %q, want nil", s, got)
		}
	}
}

// Colour codes would otherwise split one message into visually identical but
// textually distinct variants, defeating dedup, and would be replayed to the
// terminal verbatim by `quancode doctor`.
func TestMatchDeprecationsStripsANSI(t *testing.T) {
	got := MatchDeprecations("\x1b[33mwarning\x1b[0m: --old is deprecated\x1b[K\n")

	if len(got) != 1 {
		t.Fatalf("MatchDeprecations() = %q, want one match", got)
	}
	if strings.Contains(got[0], "\x1b") {
		t.Errorf("escape sequence survived: %q", got[0])
	}
	if got[0] != "warning: --old is deprecated" {
		t.Errorf("got %q", got[0])
	}
}

// Output QuanCode did not author gets replayed to a terminal later; it must
// not be able to move the cursor, clear the screen, or forge its own line.
func TestMatchDeprecationsStripsControlCharacters(t *testing.T) {
	got := MatchDeprecations("progress\r--old is deprecated\x07\x08 \x00now\n")

	if len(got) != 1 {
		t.Fatalf("MatchDeprecations() = %q, want one match", got)
	}
	for _, r := range got[0] {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			t.Errorf("control character %q survived in %q", r, got[0])
		}
	}
}

func TestMatchDeprecationsDeduplicatesIdenticalLines(t *testing.T) {
	stderr := strings.Repeat("warning: --old is deprecated\n", 20)

	if got := MatchDeprecations(stderr); len(got) != 1 {
		t.Errorf("MatchDeprecations() = %d entries, want 1", len(got))
	}
}

// A build tool the agent invoked can emit hundreds of distinct deprecation
// warnings; those must not bloat every ledger entry.
func TestMatchDeprecationsCapsCount(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("warning: api_v")
		b.WriteByte(byte('0' + i%10))
		b.WriteString("_x is deprecated\n")
	}
	if got := MatchDeprecations(b.String()); len(got) > maxDeprecations {
		t.Errorf("got %d entries, want at most %d", len(got), maxDeprecations)
	}
}

func TestMatchDeprecationsCapsLengthOnRuneBoundary(t *testing.T) {
	long := "warning: 参数已 deprecated " + strings.Repeat("参", 300)
	got := MatchDeprecations(long)

	if len(got) != 1 {
		t.Fatalf("MatchDeprecations() = %q, want one match", got)
	}
	if len(got[0]) > maxDeprecationLen+len("…") {
		t.Errorf("length %d exceeds cap", len(got[0]))
	}
	if !strings.HasSuffix(got[0], "…") {
		t.Errorf("truncated notice should be marked: %q", got[0])
	}
	// Truncating mid-rune would leave invalid UTF-8 in the ledger.
	for i, r := range got[0] {
		if r == '�' {
			t.Errorf("invalid UTF-8 at byte %d: %q", i, got[0])
		}
	}
}
