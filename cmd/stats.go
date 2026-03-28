package cmd

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/qq418716640/quancode/ledger"
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
			approvals int
		}
		stats := make(map[string]*agentStats)
		var (
			totalApprovalRequests int
			totalApproved         int
			totalDenied           int
			totalOther            int
			lastApprovalAt        time.Time
		)

		for _, e := range entries {
			if len(e.ApprovalEvents) > 0 {
				if ts, err := time.Parse(time.RFC3339, e.Timestamp); err == nil {
					if ts.After(lastApprovalAt) {
						lastApprovalAt = ts
					}
				}
			}

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
			s.approvals += len(e.ApprovalEvents)
			for _, approvalEvent := range e.ApprovalEvents {
				totalApprovalRequests++
				switch approvalEvent.Decision {
				case "approved":
					totalApproved++
				case "denied":
					totalDenied++
				default:
					totalOther++
				}
			}
		}

		// Sort agents
		var agents []string
		for a := range stats {
			agents = append(agents, a)
		}
		sort.Strings(agents)

		fmt.Printf("delegation stats (last %d days, %d total calls)\n\n", statsDays, len(entries))

		if totalApprovalRequests > 0 {
			fmt.Printf("approval summary: %d requests, %d approved, %d denied", totalApprovalRequests, totalApproved, totalDenied)
			if totalOther > 0 {
				fmt.Printf(", %d other", totalOther)
			}
			if !lastApprovalAt.IsZero() {
				fmt.Printf(", last delegation with approval at %s", lastApprovalAt.Local().Format("2006-01-02 15:04"))
			}
			fmt.Print("\n\n")
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "AGENT\tCALLS\tSUCCESS%\tFAIL\tTIMEOUT\tAVG TIME\tTOTAL TIME\tFILES\tAPPROVALS")

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
			fmt.Fprintf(w, "%s\t%d\t%s\t%d\t%d\t%s\t%s\t%d\t%d\n",
				a,
				s.calls,
				successRate,
				s.failures,
				s.timeouts,
				formatDuration(avgMs),
				formatDuration(s.totalMs),
				s.files,
				s.approvals,
			)
		}
		w.Flush()
		return nil
	},
}

func formatDuration(ms int64) string {
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

func init() {
	statsCmd.Flags().IntVar(&statsDays, "days", 30, "number of days to look back")
	rootCmd.AddCommand(statsCmd)
}
