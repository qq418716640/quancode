package cmd

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/qq418716640/quancode/ledger"
	"github.com/qq418716640/quancode/ui"
	"github.com/spf13/cobra"
)

var statsDays int

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show delegation statistics per agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		since := time.Now().AddDate(0, 0, -statsDays)
		entries, err := ledger.ReadSince(since)
		if err != nil {
			return err
		}

		if len(entries) == 0 {
			fmt.Printf("no delegation records in the last %d days.\n", statsDays)
			fmt.Println("run `quancode delegate` to start collecting data.")
			return nil
		}

		// Aggregate by agent
		type agentStats struct {
			calls     int
			successes int
			failures  int
			timeouts  int
			totalMs   int64
			files     int
		}
		stats := make(map[string]*agentStats)

		for _, e := range entries {
			s, ok := stats[e.Agent]
			if !ok {
				s = &agentStats{}
				stats[e.Agent] = s
			}
			s.calls++
			if e.ExitCode == 0 {
				s.successes++
			} else {
				s.failures++
			}
			if e.TimedOut {
				s.timeouts++
			}
			s.totalMs += e.DurationMs
			s.files += len(e.ChangedFiles)
		}

		// Sort agents
		var agents []string
		for a := range stats {
			agents = append(agents, a)
		}
		sort.Strings(agents)

		fmt.Printf("delegation stats (last %d days, %d total calls)\n\n", statsDays, len(entries))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "AGENT\tCALLS\tSUCCESS%\tFAIL\tTIMEOUT\tAVG TIME\tTOTAL TIME\tFILES")

		for _, a := range agents {
			s := stats[a]
			avgMs := int64(0)
			if s.calls > 0 {
				avgMs = s.totalMs / int64(s.calls)
			}
			successRate := ""
			if s.calls > 0 {
				successRate = fmt.Sprintf("%d%%", s.successes*100/s.calls)
			}
			fmt.Fprintf(w, "%s\t%d\t%s\t%d\t%d\t%s\t%s\t%d\n",
				a,
				s.calls,
				successRate,
				s.failures,
				s.timeouts,
				ui.FormatDuration(avgMs),
				ui.FormatDuration(s.totalMs),
				s.files,
			)
		}
		w.Flush()

		// Failure class breakdown — only shown when data exists
		printFailureBreakdown(entries)

		// Fallback analysis — only shown when run tracking data exists
		printFallbackStats(entries)

		return nil
	},
}

// printFailureBreakdown outputs failure class distribution when failures exist.
func printFailureBreakdown(entries []ledger.Entry) {
	counts := make(map[string]int)
	for _, e := range entries {
		if e.FailureClass != "" {
			counts[e.FailureClass]++
		}
	}
	if len(counts) == 0 {
		return
	}

	var classes []string
	for c := range counts {
		classes = append(classes, c)
	}
	sort.Strings(classes)

	fmt.Print("\nfailure breakdown: ")
	for i, c := range classes {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Printf("%s=%d", c, counts[c])
	}
	fmt.Println()
}

// printFallbackStats outputs fallback chain analysis when run tracking data exists.
func printFallbackStats(entries []ledger.Entry) {
	// Group entries by RunID
	runs := make(map[string][]ledger.Entry)
	for _, e := range entries {
		if e.RunID == "" {
			continue
		}
		runs[e.RunID] = append(runs[e.RunID], e)
	}

	if len(runs) == 0 {
		return
	}

	// Analyze runs with fallback (>1 attempt)
	var totalRuns, fallbackRuns, fallbackSuccesses int
	reasonCounts := make(map[string]int)
	chainCounts := make(map[string]int)

	for _, attempts := range runs {
		totalRuns++

		// Ensure attempts are ordered by Attempt number
		sort.Slice(attempts, func(i, j int) bool {
			return attempts[i].Attempt < attempts[j].Attempt
		})

		if len(attempts) < 2 {
			continue
		}
		fallbackRuns++

		// Check if the final attempt succeeded
		last := attempts[len(attempts)-1]
		if last.ExitCode == 0 && !last.TimedOut {
			fallbackSuccesses++
		}

		// Count fallback reasons and agent chains
		for _, a := range attempts {
			if a.FallbackReason != "" {
				reasonCounts[a.FallbackReason]++
			}
		}

		// Build chain string: alpha → beta → ...
		chain := attempts[0].Agent
		for i := 1; i < len(attempts); i++ {
			chain += " → " + attempts[i].Agent
		}
		chainCounts[chain]++
	}

	if fallbackRuns == 0 {
		return
	}

	fmt.Printf("\nfallback analysis: %d/%d runs triggered fallback, %d%% recovered\n",
		fallbackRuns, totalRuns, fallbackSuccesses*100/fallbackRuns)

	// Reason breakdown
	if len(reasonCounts) > 0 {
		var reasons []string
		for r := range reasonCounts {
			reasons = append(reasons, r)
		}
		sort.Strings(reasons)

		fmt.Print("  reasons: ")
		for i, r := range reasons {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%s=%d", r, reasonCounts[r])
		}
		fmt.Println()
	}

	// Top chains
	if len(chainCounts) > 0 {
		// Sort chains by frequency
		type chainFreq struct {
			chain string
			count int
		}
		var chains []chainFreq
		for c, n := range chainCounts {
			chains = append(chains, chainFreq{c, n})
		}
		sort.Slice(chains, func(i, j int) bool {
			if chains[i].count != chains[j].count {
				return chains[i].count > chains[j].count
			}
			return chains[i].chain < chains[j].chain
		})

		fmt.Print("  chains: ")
		for i, cf := range chains {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%s (%d)", cf.chain, cf.count)
			if i >= 4 {
				break
			}
		}
		fmt.Println()
	}
}

func init() {
	statsCmd.Flags().IntVar(&statsDays, "days", 30, "number of days to look back")
	rootCmd.AddCommand(statsCmd)
}
