package ui

import (
	"fmt"
	"os"
	"strings"
)

// DelegationStart prints a formatted delegation start message to stderr.
func DelegationStart(agentKey, task, isolation string) {
	// Truncate task for display (keep first line, max 80 chars)
	displayTask := firstLine(task, 80)

	fmt.Fprintf(os.Stderr, "[quancode] ⚡ Dispatching to %s...\n", agentKey)
	fmt.Fprintf(os.Stderr, "[quancode]    Task: %s\n", displayTask)
	if isolation != "" && isolation != "inplace" {
		fmt.Fprintf(os.Stderr, "[quancode]    Mode: %s\n", isolation)
	}
}

// DelegationSuccess prints a formatted delegation success message to stderr.
func DelegationSuccess(agentKey string, durationMs int64, changedFiles int) {
	dur := FormatDuration(durationMs)
	if changedFiles > 0 {
		fmt.Fprintf(os.Stderr, "[quancode] ✓ %s completed in %s — %d file(s) changed\n", agentKey, dur, changedFiles)
	} else {
		fmt.Fprintf(os.Stderr, "[quancode] ✓ %s completed in %s\n", agentKey, dur)
	}
}

// DelegationFailure prints a formatted delegation failure message to stderr.
func DelegationFailure(agentKey string, durationMs int64, failureClass string) {
	dur := FormatDuration(durationMs)
	symbol := "✗"
	if failureClass == "timed_out" {
		symbol = "⏱"
	}
	fmt.Fprintf(os.Stderr, "[quancode] %s %s failed in %s (%s)\n", symbol, agentKey, dur, failureClass)
}

// FallbackChain prints the full delegation chain when fallback occurred.
// chain is a list of (agentKey, failureClass) pairs, with the last entry
// being the final agent (failureClass empty if successful).
func FallbackChain(chain []ChainLink) {
	if len(chain) <= 1 {
		return
	}
	var parts []string
	for _, link := range chain {
		if link.FailureClass == "" {
			parts = append(parts, link.Agent+" ✓")
		} else {
			parts = append(parts, fmt.Sprintf("%s (%s)", link.Agent, link.FailureClass))
		}
	}
	fmt.Fprintf(os.Stderr, "[quancode] Chain: %s\n", strings.Join(parts, " → "))
}

// ChainLink represents one step in a delegation fallback chain.
type ChainLink struct {
	Agent        string
	FailureClass string
}

func firstLine(s string, maxLen int) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	if len(s) > maxLen {
		if maxLen <= 3 {
			return s[:maxLen]
		}
		return s[:maxLen-3] + "..."
	}
	return s
}

// FormatDuration formats milliseconds into a human-readable duration string.
func FormatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	secs := float64(ms) / 1000
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	mins := int(secs) / 60
	remainSecs := int(secs) % 60
	return fmt.Sprintf("%dm%ds", mins, remainSecs)
}
