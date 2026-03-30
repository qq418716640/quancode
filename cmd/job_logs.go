package cmd

import (
	"fmt"
	"os"

	"github.com/qq418716640/quancode/job"
	"github.com/spf13/cobra"
)

var jobLogsTail int

var jobLogsCmd = &cobra.Command{
	Use:   "logs <job_id>",
	Short: "Show output logs of an async job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]
		state, err := job.ReadState(jobID)
		if err != nil {
			return fmt.Errorf("read job %s: %w", jobID, err)
		}

		if state.OutputFile == "" {
			fmt.Fprintln(os.Stderr, "no output file for this job")
			return nil
		}

		data, err := os.ReadFile(state.OutputFile)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintln(os.Stderr, "output file not yet created (job may still be starting)")
				return nil
			}
			return fmt.Errorf("read output file: %w", err)
		}

		output := string(data)
		if jobLogsTail > 0 {
			output = tailLines(output, jobLogsTail)
		}
		fmt.Print(output)
		return nil
	},
}

func init() {
	jobLogsCmd.Flags().IntVar(&jobLogsTail, "tail", 0, "show only the last N lines (0 = all)")
	jobCmd.AddCommand(jobLogsCmd)
}
