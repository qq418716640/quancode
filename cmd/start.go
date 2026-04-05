package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/config"
	"github.com/qq418716640/quancode/dashboard"
	"github.com/qq418716640/quancode/prompt"
	"github.com/qq418716640/quancode/ui"
	"github.com/qq418716640/quancode/version"
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

		// Set terminal title (OSC 2) — best-effort, harmless on unsupported terminals.
		repoName := filepath.Base(workDir)
		fmt.Fprintf(os.Stderr, "\033]2;QuanCode: %s - %s\007", primary, repoName)

		// Startup banner
		promptMode := ac.PromptMode
		if promptMode == "" {
			promptMode = "append_arg"
		}
		fmt.Fprintf(os.Stderr, "[quancode] session active (%s)\n", version.Version)
		fmt.Fprintf(os.Stderr, "[quancode] primary: %s (%s)\n", primary, ac.Name)
		fmt.Fprintf(os.Stderr, "[quancode] prompt:  %s\n", promptMode)

		// Dashboard auto-start / tip
		handleDashboard(cfg)

		// Use the agent's LaunchAsPrimary which handles prompt_mode
		return a.LaunchAsPrimary(workDir, systemPrompt)
	},
}

// completeAgentKeys returns enabled agent keys from config for shell completion.
func completeAgentKeys(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var keys []string
	for key, ac := range cfg.Agents {
		if ac.Enabled {
			keys = append(keys, key)
		}
	}
	return keys, cobra.ShellCompDirectiveNoFileComp
}

// handleDashboard manages dashboard auto-start and first-use tips.
func handleDashboard(cfg *config.Config) {
	port := cfg.Preferences.EffectiveDashboardPort()

	switch cfg.Preferences.DashboardMode {
	case "auto":
		url, started, err := dashboard.EnsureRunning(port)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[quancode] warning: dashboard auto-start failed: %v\n", err)
			return
		}
		if started {
			fmt.Fprintf(os.Stderr, "[quancode] dashboard started: %s\n", url)
		} else {
			fmt.Fprintf(os.Stderr, "[quancode] dashboard: %s\n", url)
		}
	case "off":
		// User explicitly disabled — do nothing.
	default:
		// Undecided — show a one-time tip (only in interactive terminals).
		if ui.IsTTY() {
			fmt.Fprintf(os.Stderr, "[quancode] tip: run \"quancode dashboard enable\" to auto-start the web dashboard\n")
		}
	}
}

func init() {
	startCmd.Flags().StringVar(&primaryAgent, "primary", "", "primary CLI agent (default from config)")
	_ = startCmd.RegisterFlagCompletionFunc("primary", completeAgentKeys)
	rootCmd.AddCommand(startCmd)
}
