package cmd

import (
	"fmt"
	"time"

	"github.com/qq418716640/quancode/job"
	"github.com/spf13/cobra"
)

var jobCleanTTL string

var jobCleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove expired job files",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ttl, err := time.ParseDuration(jobCleanTTL)
		if err != nil {
			return fmt.Errorf("invalid --ttl: %w", err)
		}

		protectionPeriod := 30 * time.Second
		result, err := job.Clean(ttl, protectionPeriod)
		if err != nil {
			return err
		}

		fmt.Printf("removed %d job(s), skipped %d\n", result.Removed, result.Skipped)
		for _, e := range result.Errors {
			fmt.Printf("  error: %s\n", e)
		}
		return nil
	},
}

func init() {
	jobCleanCmd.Flags().StringVar(&jobCleanTTL, "ttl", "168h", "remove jobs older than this duration (default: 7 days)")
	jobCmd.AddCommand(jobCleanCmd)
}
