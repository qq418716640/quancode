package cmd

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/qq418716640/quancode/job"
	"github.com/spf13/cobra"
)

var jobCancelCmd = &cobra.Command{
	Use:   "cancel <job_id>",
	Short: "Cancel a running async job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]
		state, err := job.ReadState(jobID)
		if err != nil {
			return fmt.Errorf("read job %s: %w", jobID, err)
		}

		// Idempotent: already terminal is not an error.
		if job.IsTerminal(state.Status) {
			fmt.Fprintf(os.Stderr, "job %s is already %s\n", jobID, state.Status)
			return nil
		}

		// Send SIGTERM to the process group.
		if state.PID > 0 && job.IsProcessAlive(state.PID, state.PIDStartTime) {
			if err := syscall.Kill(-state.PID, syscall.SIGTERM); err != nil {
				if err2 := syscall.Kill(state.PID, syscall.SIGTERM); err2 != nil {
					fmt.Fprintf(os.Stderr, "[quancode] warning: failed to send SIGTERM: %v\n", err2)
				}
			}
			fmt.Fprintf(os.Stderr, "[quancode] sent SIGTERM to job %s (pid %d)\n", jobID, state.PID)

			// Wait briefly for the process to handle SIGTERM.
			time.Sleep(2 * time.Second)

			// Check if still alive, SIGKILL if needed.
			if job.IsProcessAlive(state.PID, state.PIDStartTime) {
				syscall.Kill(-state.PID, syscall.SIGKILL)
				fmt.Fprintf(os.Stderr, "[quancode] sent SIGKILL to job %s\n", jobID)
			}
		}

		// Mark as cancelled (re-read state in case runner updated it).
		state, err = job.ReadState(jobID)
		if err != nil {
			return fmt.Errorf("re-read job %s: %w", jobID, err)
		}
		if job.IsTerminal(state.Status) {
			fmt.Fprintf(os.Stderr, "job %s finished as %s during cancel\n", jobID, state.Status)
			return nil
		}

		state.Status = job.StatusCancelled
		state.ErrorCode = job.ErrCodeCancelled
		state.Error = "cancelled by user"
		state.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		if err := job.WriteState(state); err != nil {
			// CAS conflict or terminal-status guard: runner may have finished concurrently.
			if latest, readErr := job.ReadState(jobID); readErr == nil && job.IsTerminal(latest.Status) {
				fmt.Fprintf(os.Stderr, "job %s finished as %s during cancel\n", jobID, latest.Status)
				return nil
			}
			return fmt.Errorf("mark job cancelled: %w", err)
		}

		fmt.Fprintf(os.Stderr, "[quancode] job %s cancelled\n", jobID)
		return nil
	},
}

func init() {
	jobCmd.AddCommand(jobCancelCmd)
}
