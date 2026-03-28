package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/config"
	"github.com/spf13/cobra"
)

var agentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "List available AI coding CLIs and their status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "AGENT\tSTATUS\tCOMMAND\tSTRENGTHS\tDESCRIPTION")

		var keys []string
		for key := range cfg.Agents {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			ac := cfg.Agents[key]
			if !ac.Enabled {
				continue
			}
			a := agent.FromConfig(key, ac)
			status := "unavailable"
			if ok, _ := a.IsAvailable(); ok {
				status = "available"
			}
			strengths := strings.Join(ac.Strengths, ", ")
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", key, status, ac.Command, strengths, ac.Description)
		}
		w.Flush()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(agentsCmd)
}
