package cmd

import (
	"github.com/spf13/cobra"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage async delegation jobs",
}

func init() {
	rootCmd.AddCommand(jobCmd)
}
