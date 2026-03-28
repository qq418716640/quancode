package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/config"
	"github.com/qq418716640/quancode/prompt"
	"github.com/spf13/cobra"
)

var primaryAgent string

var startCmd = &cobra.Command{
	Use:          "start",
	Short:        "Launch a primary CLI with sub-agent delegation capability",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return err
		}

		// Resolve primary agent
		primary := primaryAgent
		if primary == "" {
			primary = cfg.DefaultPrimary
		}

		ac, ok := cfg.Agents[primary]
		if !ok {
			return fmt.Errorf("unknown primary agent: %s", primary)
		}
		if !ac.Enabled {
			return fmt.Errorf("primary agent %s is disabled", primary)
		}

		a := agent.FromConfig(primary, ac)
		if avail, _ := a.IsAvailable(); !avail {
			return fmt.Errorf("primary agent %s: command %q not found in PATH", primary, ac.Command)
		}

		// Resolve quancode binary path for injection prompt
		quancodeBin := "quancode"
		if _, err := exec.LookPath("quancode"); err != nil {
			if selfPath, e := os.Executable(); e == nil {
				quancodeBin = selfPath
			}
		}

		// Build injection prompt
		systemPrompt, err := prompt.Build(cfg, primary, quancodeBin)
		if err != nil {
			return fmt.Errorf("build system prompt: %w", err)
		}

		workDir, _ := os.Getwd()

		fmt.Fprintf(os.Stderr, "[quancode] starting %s as primary agent with delegation enabled\n", primary)

		// Use the agent's LaunchAsPrimary which handles prompt_mode
		return a.LaunchAsPrimary(workDir, systemPrompt)
	},
}

func init() {
	startCmd.Flags().StringVar(&primaryAgent, "primary", "", "primary CLI agent (default from config)")
	rootCmd.AddCommand(startCmd)
}
