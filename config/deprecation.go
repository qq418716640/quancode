package config

import (
	"regexp"
	"strings"
)

// deprecationStem is matched case-insensitively against each line of an
// agent's stderr. One stem covers "deprecated", "deprecation",
// "deprecating", and "DeprecationWarning" — note that "deprecated" alone
// would miss "DeprecationWarning", which is how Python-based CLIs phrase it.
//
// This is deliberately the opposite policy from CommonFailurePatterns, whose
// doc says to paste the CLI's real message verbatim. That table drives the
// fallback decision, so it has to be precise. This one exists because the
// failure it addresses *was not knowing a deprecation existed*: codex began
// warning that --full-auto was deprecated and QuanCode kept passing the flag
// for weeks. By the time the exact wording is known, the discovery this is
// meant to make has already happened by other means.
//
// Recall is therefore worth more than precision here. The cost of a false
// positive is one advisory line quoting output the agent really did produce;
// the cost of a false negative is another silent flag rot.
const deprecationStem = "deprecat"

// ansiEscape matches the CSI/OSC sequences CLIs use for colour and cursor
// control. Left in, they would split a message into visually identical but
// textually distinct variants and would be replayed verbatim by `doctor`.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)

const (
	// maxDeprecationLen caps one notice. Agents can emit very long stderr
	// lines (stack traces, embedded JSON); a notice is a pointer to go look,
	// not a transcript.
	maxDeprecationLen = 200
	// maxDeprecations caps how many are kept per run, so a build tool
	// spewing hundreds of deprecation warnings cannot bloat every ledger
	// entry for the rest of the session.
	maxDeprecations = 5
)

// MatchDeprecations extracts lifecycle warnings from an agent's stderr.
//
// Matching is per line, and the matched line itself is what gets reported —
// never the surrounding buffer. QuanCode cannot tell whether a line came
// from the CLI's own argument parser or from something the CLI shelled out
// to, so quoting the exact line lets the reader judge, and keeps callers
// from claiming more than was observed.
//
// Only stderr should be passed. An agent's stdout routinely discusses
// deprecation because that is a normal thing to ask an agent about
// ("find deprecated APIs in this package"), and scanning it would turn every
// such task into a false report.
func MatchDeprecations(stderr string) []string {
	if stderr == "" {
		return nil
	}

	var out []string
	seen := make(map[string]bool)

	for _, line := range strings.Split(stderr, "\n") {
		line = sanitizeNoticeLine(line)
		if line == "" || !strings.Contains(strings.ToLower(line), deprecationStem) {
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
		if len(out) == maxDeprecations {
			break
		}
	}
	return out
}

// sanitizeNoticeLine reduces one stderr line to something safe to persist
// and to print back out later. Control characters are dropped rather than
// escaped: a notice is replayed to a terminal by `quancode doctor`, and
// output QuanCode did not author should not be able to move the cursor,
// clear the screen, or forge a line of its own.
func sanitizeNoticeLine(line string) string {
	line = ansiEscape.ReplaceAllString(line, "")
	line = strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		// C0 controls and DEL, plus the C1 range some terminals honour.
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, line)
	line = strings.Join(strings.Fields(line), " ")

	if len(line) > maxDeprecationLen {
		// Cut on a rune boundary so the stored text stays valid UTF-8.
		cut := maxDeprecationLen
		for cut > 0 && !isUTF8Start(line[cut]) {
			cut--
		}
		line = line[:cut] + "…"
	}
	return line
}

func isUTF8Start(b byte) bool { return b&0xC0 != 0x80 }
