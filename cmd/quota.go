package cmd

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/qq418716640/quancode/ledger"
	"github.com/spf13/cobra"
)

var (
	quotaSetAgent string
	quotaSetLimit int
	quotaSetUnit  string
	quotaSetMode  string
	quotaSetDay   int
	quotaSetHours int
	quotaSetNotes string
)

var quotaCmd = &cobra.Command{
	Use:   "quota",
	Short: "View or set agent quota limits and current usage",
	Long: `View or set quota limits per agent.

Examples:
  # Claude Max: 5-hour rolling window
  quancode quota --set-agent claude --unit hours --limit 5 --reset-mode rolling_hours --rolling-hours 5 --notes "Claude Max"

  # Codex Pro: weekly reset
  quancode quota --set-agent codex --unit calls --limit 200 --reset-mode weekly --reset-day 1 --notes "Codex Pro"

  # Monthly call limit
  quancode quota --set-agent aider --unit calls --limit 500 --reset-mode monthly --reset-day 1`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if quotaSetAgent != "" {
			return setQuota(cmd)
		}
		return showQuota()
	},
}

func setQuota(cmd *cobra.Command) error {
	qc, err := ledger.LoadQuota()
	if err != nil {
		return err
	}

	limitSet := cmd.Flags().Changed("limit")
	unitSet := cmd.Flags().Changed("unit")
	modeSet := cmd.Flags().Changed("reset-mode")
	daySet := cmd.Flags().Changed("reset-day")
	hoursSet := cmd.Flags().Changed("rolling-hours")
	notesSet := cmd.Flags().Changed("notes")

	if !limitSet && !unitSet && !modeSet && !daySet && !hoursSet && !notesSet {
		return fmt.Errorf("specify at least one setting flag")
	}

	aq := qc.Agents[quotaSetAgent]

	if unitSet {
		switch quotaSetUnit {
		case "calls", "minutes", "hours":
			aq.Unit = quotaSetUnit
		default:
			return fmt.Errorf("--unit must be calls, minutes, or hours")
		}
	}
	if limitSet {
		if quotaSetLimit < 0 {
			return fmt.Errorf("--limit must be >= 0 (0 = unlimited)")
		}
		aq.Limit = quotaSetLimit
	}
	if modeSet {
		switch quotaSetMode {
		case "monthly", "weekly", "rolling_hours":
			aq.ResetMode = quotaSetMode
		default:
			return fmt.Errorf("--reset-mode must be monthly, weekly, or rolling_hours")
		}
	}
	if daySet {
		mode := aq.ResetMode
		if modeSet {
			mode = quotaSetMode
		}
		if mode == "weekly" {
			if quotaSetDay < 1 || quotaSetDay > 7 {
				return fmt.Errorf("--reset-day for weekly must be 1 (Mon) to 7 (Sun)")
			}
		} else {
			if quotaSetDay < 1 || quotaSetDay > 28 {
				return fmt.Errorf("--reset-day for monthly must be 1 to 28")
			}
		}
		aq.ResetDay = quotaSetDay
	}
	if hoursSet {
		if quotaSetHours < 1 {
			return fmt.Errorf("--rolling-hours must be >= 1")
		}
		aq.RollingHours = quotaSetHours
	}
	if notesSet {
		aq.Notes = quotaSetNotes
	}

	qc.Agents[quotaSetAgent] = aq
	if err := ledger.SaveQuota(qc); err != nil {
		return err
	}

	fmt.Printf("quota updated for %s: %d %s per %s\n",
		quotaSetAgent, aq.Limit, aq.Unit, aq.ResetMode)
	return nil
}

func showQuota() error {
	qc, err := ledger.LoadQuota()
	if err != nil {
		return err
	}

	if len(qc.Agents) == 0 {
		fmt.Println("no quotas configured. examples:")
		fmt.Println("  quancode quota --set-agent claude --unit hours --limit 5 --reset-mode rolling_hours --rolling-hours 5 --notes \"Claude Max\"")
		fmt.Println("  quancode quota --set-agent codex --unit calls --limit 200 --reset-mode weekly --reset-day 1 --notes \"Codex Pro\"")
		return nil
	}

	var agents []string
	for a := range qc.Agents {
		agents = append(agents, a)
	}
	sort.Strings(agents)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "AGENT\tLIMIT\tUNIT\tUSED\tREMAINING\tPERIOD\tNOTES")

	for _, a := range agents {
		aq := qc.Agents[a]
		used, since := aq.Usage(a)

		unit := aq.Unit
		if unit == "" {
			unit = "calls"
		}

		limitStr := "unlimited"
		remainStr := "-"
		if aq.Limit > 0 {
			limitStr = fmt.Sprintf("%d", aq.Limit)
			remain := float64(aq.Limit) - used
			if remain < 0 {
				remain = 0
			}
			remainStr = formatUsage(remain, unit)
		}

		period := aq.ResetMode
		if period == "" {
			period = "monthly"
		}
		if period == "rolling_hours" {
			period = fmt.Sprintf("rolling %dh", aq.RollingHours)
		}
		periodInfo := fmt.Sprintf("%s (since %s)", period, since.Format("01-02 15:04"))

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			a, limitStr, unit, formatUsage(used, unit), remainStr, periodInfo, aq.Notes)
	}
	w.Flush()
	return nil
}

func formatUsage(val float64, unit string) string {
	switch unit {
	case "hours":
		if val < 1 {
			return fmt.Sprintf("%.0fm", val*60)
		}
		return fmt.Sprintf("%.1fh", val)
	case "minutes":
		return fmt.Sprintf("%.1fm", val)
	default:
		return fmt.Sprintf("%.0f", val)
	}
}

func init() {
	quotaCmd.Flags().StringVar(&quotaSetAgent, "set-agent", "", "agent to set quota for")
	quotaCmd.Flags().IntVar(&quotaSetLimit, "limit", 0, "quota limit per period")
	quotaCmd.Flags().StringVar(&quotaSetUnit, "unit", "", "quota unit: calls, minutes, or hours")
	quotaCmd.Flags().StringVar(&quotaSetMode, "reset-mode", "", "reset mode: monthly, weekly, or rolling_hours")
	quotaCmd.Flags().IntVar(&quotaSetDay, "reset-day", 0, "reset day (1-28 for monthly, 1-7 for weekly)")
	quotaCmd.Flags().IntVar(&quotaSetHours, "rolling-hours", 0, "window size for rolling_hours mode")
	quotaCmd.Flags().StringVar(&quotaSetNotes, "notes", "", "description (e.g. 'Claude Max plan')")
	rootCmd.AddCommand(quotaCmd)
}
