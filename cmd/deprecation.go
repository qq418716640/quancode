package cmd

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/qq418716640/quancode/ledger"
)

// Deprecation notices are printed at most once per process per (agent,
// message).
//
// Once per *process* rather than once per day, which is what a first draft
// of this proposed. A daily budget needs cross-process state, and deriving
// it from the ledger — the trick the health breaker uses — does not work
// here: the check would be a read-then-print race between concurrent
// delegations, and worse, an async job or a `--quiet` batch item would
// consume the day's single notice without anything being shown to a human.
//
// Per-process has none of that. It still collapses the case that matters —
// a 500-item batch prints one line, not 500 — and a notice that keeps
// reappearing across separate commands is a flag that is still deprecated,
// which is the correct thing to keep saying. `quancode doctor` reads the
// full history back out of the ledger.
var (
	deprecationSeenMu sync.Mutex
	deprecationSeen   = map[string]bool{}
)

// maxDeprecationKeys bounds the dedup set. Each delegation contributes at
// most config's per-run cap, but a long batch whose agent shells out to a
// build tool could still produce a new distinct message every item. Past
// this ceiling the mechanism goes quiet rather than growing without limit:
// anything generating thousands of distinct deprecation lines is noise, and
// the ledger has kept them all regardless.
const maxDeprecationKeys = 1000

// noteDeprecationOnce reports whether this (agent, message) pair should be
// printed now, and records it so later attempts in this process stay quiet.
func noteDeprecationOnce(agentKey, message string) bool {
	key := agentKey + "\x00" + message
	deprecationSeenMu.Lock()
	defer deprecationSeenMu.Unlock()
	if deprecationSeen[key] {
		return false
	}
	if len(deprecationSeen) >= maxDeprecationKeys {
		return false
	}
	deprecationSeen[key] = true
	return true
}

// printDeprecations writes an agent's lifecycle notices to stderr, at most
// once per process each.
//
// Only call this from code that owns the terminal. Claiming the
// once-per-process slot and then discarding the line — which is what
// happens if a caller that cannot print calls this anyway — silently mutes
// the notice for every later attempt in the same process. Attempts run by
// review-set, speculative, and the async job runner are all `Quiet` and go
// through their orchestrator instead.
//
// The matched line is quoted verbatim and attributed to the agent rather
// than paraphrased: QuanCode cannot tell whether it came from the CLI's own
// argument parsing or from something the CLI shelled out to, so the reader
// gets the evidence and decides.
func printDeprecations(agentKey string, notices []string) {
	for _, d := range notices {
		if noteDeprecationOnce(agentKey, d) {
			fmt.Fprintf(os.Stderr, "[quancode] %s reported: %s\n", agentKey, d)
		}
	}
}

// deprecationRecord aggregates one distinct (agent, message) pair across the
// ledger window `doctor` reports on.
type deprecationRecord struct {
	Agent     string
	Message   string
	Count     int
	FirstSeen time.Time
	LastSeen  time.Time
}

// recentDeprecations collects deprecation notices recorded in the last
// `window`, newest activity first.
//
// A read failure is reported separately from an empty result rather than
// collapsed into it. `doctor` must not fail because the ledger is
// unreadable — that says nothing about whether the agents work — but
// silently showing no notices would let a broken ledger look exactly like a
// clean bill of health, in the one command whose job is to tell them apart.
func recentDeprecations(window time.Duration) (records []deprecationRecord, readErr error) {
	entries, err := ledger.ReadSince(time.Now().Add(-window))
	if err != nil {
		return nil, err
	}

	byKey := map[string]*deprecationRecord{}
	for _, e := range entries {
		if len(e.Deprecations) == 0 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			continue
		}
		for _, msg := range e.Deprecations {
			key := e.Agent + "\x00" + msg
			r, ok := byKey[key]
			if !ok {
				r = &deprecationRecord{Agent: e.Agent, Message: msg, FirstSeen: ts, LastSeen: ts}
				byKey[key] = r
			}
			r.Count++
			if ts.Before(r.FirstSeen) {
				r.FirstSeen = ts
			}
			if ts.After(r.LastSeen) {
				r.LastSeen = ts
			}
		}
	}

	out := make([]deprecationRecord, 0, len(byKey))
	for _, r := range byKey {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].Agent < out[j].Agent
	})
	return out, nil
}

// humanizeSince renders how long ago a notice was last seen, so a reader can
// tell "this is still happening" from "this was fixed last week" without
// doing date arithmetic.
func humanizeSince(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
