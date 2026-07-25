package cmd

import (
	"fmt"
	"strings"

	"github.com/qq418716640/quancode/config"
	"github.com/qq418716640/quancode/health"
	"github.com/qq418716640/quancode/router"
	"github.com/spf13/cobra"
)

var routeCmd = &cobra.Command{
	Use:   "route [task description]",
	Short: "Preview which agent would be selected for a task",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return err
		}

		task := strings.Join(args, " ")
		// Preview must apply the same health rules as a real delegation,
		// otherwise it reports an agent that delegate would skip.
		hs := health.NewSnapshot(cfg.Preferences.AgentHealth)
		sel, probed, skipped := router.SelectHealthy(cfg, task, nil, hs, true)

		if sel == nil {
			fmt.Println("no available sub-agent found")
			return nil
		}

		ac := cfg.Agents[sel.AgentKey]
		fmt.Printf("task:   %s\n", task)
		fmt.Printf("agent:  %s (%s)\n", sel.AgentKey, ac.Name)
		if probed {
			fmt.Printf("reason: all agents unhealthy — would probe this one anyway\n")
		} else {
			fmt.Printf("reason: %s\n", sel.Reason)
		}
		for key, reason := range skipped {
			fmt.Printf("skipped: %s — %s\n", key, reason)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(routeCmd)
}
