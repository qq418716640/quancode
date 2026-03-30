package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/qq418716640/quancode/job"
	"github.com/spf13/cobra"
)

var jobStatusFormat string

var jobStatusCmd = &cobra.Command{
	Use:   "status <job_id>",
	Short: "Show status of an async job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]
		state, err := job.ReadState(jobID)
		if err != nil {
			return fmt.Errorf("read job %s: %w", jobID, err)
		}
		state = job.DetectLost(state)

		if jobStatusFormat == "json" {
			data, _ := json.MarshalIndent(state, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		fmt.Fprintf(os.Stdout, "Job:       %s\n", state.JobID)
		fmt.Fprintf(os.Stdout, "Status:    %s\n", state.Status)
		fmt.Fprintf(os.Stdout, "Agent:     %s\n", agentDisplay(state))
		fmt.Fprintf(os.Stdout, "Task:      %s\n", state.Task)
		fmt.Fprintf(os.Stdout, "Isolation: %s\n", state.Isolation)
		fmt.Fprintf(os.Stdout, "WorkDir:   %s\n", state.WorkDir)
		fmt.Fprintf(os.Stdout, "Created:   %s\n", formatTime(state.CreatedAt))
		if state.FinishedAt != "" {
			fmt.Fprintf(os.Stdout, "Finished:  %s\n", formatTime(state.FinishedAt))
		}
		if state.ErrorCode != "" {
			fmt.Fprintf(os.Stdout, "Error:     [%s] %s\n", state.ErrorCode, state.Error)
		}
		if state.ExitCode != nil {
			fmt.Fprintf(os.Stdout, "ExitCode:  %d\n", *state.ExitCode)
		}
		if len(state.ChangedFiles) > 0 {
			fmt.Fprintf(os.Stdout, "Changed:   %d files\n", len(state.ChangedFiles))
		}
		if state.PatchFile != "" {
			fmt.Fprintf(os.Stdout, "Patch:     %s\n", state.PatchFile)
		}
		return nil
	},
}

func formatTime(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Local().Format("2006-01-02 15:04:05")
	}
	return s
}

func formatTimeShort(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Local().Format("01-02 15:04")
	}
	return s
}

func init() {
	jobStatusCmd.Flags().StringVar(&jobStatusFormat, "format", "text", "output format: text or json")
	jobCmd.AddCommand(jobStatusCmd)
}
