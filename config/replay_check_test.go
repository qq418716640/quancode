package config

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestReplayHistoricalFailures re-classifies every recorded failure in the
// local ledger against the current pattern table. It is a calibration aid,
// not a correctness gate: it skips when no ledger exists (CI, fresh installs)
// and only reports. Run with -v to see the breakdown.
//
//	go test ./config/ -run ReplayHistorical -v
func TestReplayHistoricalFailures(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	logDir := filepath.Join(home, ".config", "quancode", "logs")
	files, _ := filepath.Glob(filepath.Join(logDir, "*.jsonl"))
	if len(files) == 0 {
		t.Skip("no local ledger to replay")
	}

	type entry struct {
		Agent        string `json:"agent"`
		ExitCode     int    `json:"exit_code"`
		TimedOut     bool   `json:"timed_out"`
		Cancelled    bool   `json:"cancelled"`
		FailureClass string `json:"failure_class"`
		OutputFile   string `json:"output_file"`
	}

	var total, transient, hinted, unexplained int
	byAgent := map[string]int{}

	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
		for sc.Scan() {
			var e entry
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue
			}
			// Only failures the old code called agent_failed — the ones that
			// silently skipped fallback.
			if e.FailureClass != "agent_failed" || e.Cancelled {
				continue
			}
			out, err := os.ReadFile(e.OutputFile)
			if err != nil {
				continue
			}
			total++
			hints, isTransient := MatchFailurePatterns(string(out), nil)
			if isTransient {
				transient++
				byAgent[e.Agent]++
			}
			if len(hints) > 0 {
				hinted++
			} else {
				unexplained++
			}
		}
		fh.Close()
	}

	if total == 0 {
		t.Skip("no agent_failed entries with captured output")
	}

	agents := make([]string, 0, len(byAgent))
	for a := range byAgent {
		agents = append(agents, a)
	}
	sort.Strings(agents)

	t.Logf("replayed %d historical agent_failed attempts", total)
	t.Logf("  now classified transient (would fall back): %d (%.0f%%)", transient, float64(transient)/float64(total)*100)
	t.Logf("  now produce a diagnostic hint:              %d (%.0f%%)", hinted, float64(hinted)/float64(total)*100)
	t.Logf("  still unexplained (no pattern matched):     %d", unexplained)
	for _, a := range agents {
		t.Logf("  transient by agent: %-10s %d", a, byAgent[a])
	}
}
