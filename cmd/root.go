package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/qq418716640/quancode/agent"
	"github.com/qq418716640/quancode/version"
	"github.com/spf13/cobra"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:           "quancode",
	Short:         "Unified CLI orchestrator for AI coding agents",
	Long:          "QuanCode launches a primary AI coding CLI and enables it to delegate tasks to other CLIs as sub-agents.",
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		go version.BackgroundUpdate()
	},
}

func Execute() {
	err := rootCmd.Execute()

	// Show update notice after command completes
	if notice := version.UpdateNotice(); notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}

	if err != nil {
		var exitErr *agent.ExitStatusError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/quancode/quancode.yaml)")
}
