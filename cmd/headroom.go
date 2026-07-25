package cmd

import (
	"fmt"
	"os"

	"github.com/qq418716640/quancode/health"
)

// headroomRatio is the fraction of the configured timeout that recent
// successful runs may reach before we warn. 0.8 means "p90 of successes is
// already using 80% of the budget".
const headroomRatio = 0.8

// warnTimeoutHeadroom warns when an agent's recent successful runs are
// pressing against its configured timeout, which is what actually predicts
// timeouts.
//
// Replaces the v0.8.24 task-size warning. Across 1279 attempts in the
// 2026-05..07 review, 138 of 152 timeouts landed exactly on the configured
// ceiling while codex's successful runs reached p90=412s and p99=474s
// against a 480s cap — the work genuinely needs most of the budget, and
// anything slightly slower dies. Task length showed no such relationship.
//
// Returns the warning text (also printed to stderr unless quiet), or "".
func warnTimeoutHeadroom(hs *health.Snapshot, agentKey string, effectiveTimeout int, quiet bool) string {
	if hs == nil || effectiveTimeout <= 0 {
		return ""
	}
	p90 := hs.Get(agentKey).SuccessDurationP90()
	if p90 <= 0 || float64(p90) < float64(effectiveTimeout)*headroomRatio {
		return ""
	}
	suggested := roundUpTo(p90*2, 60)
	msg := fmt.Sprintf(
		"%s recently needed up to %ds (p90 of successful runs) against a %ds timeout — only %d%% headroom. Consider --timeout %d, or split the task.",
		agentKey, p90, effectiveTimeout, int(100-float64(p90)/float64(effectiveTimeout)*100), suggested)
	if !quiet {
		fmt.Fprintf(os.Stderr, "[quancode] warning: %s\n", msg)
	}
	return msg
}

func roundUpTo(v, unit int) int {
	if unit <= 0 {
		return v
	}
	if r := v % unit; r != 0 {
		v += unit - r
	}
	return v
}
