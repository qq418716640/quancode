package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/qq418716640/quancode/job"
	"github.com/spf13/cobra"
)

var (
	jobResultFormat       string
	jobResultOutputTail   int
)

// jobResultOutput is the JSON structure returned by `job result`.
type jobResultOutput struct {
	JobID        string   `json:"job_id"`
	Status       string   `json:"status"`
	Agent        string   `json:"agent"`
	ActualAgent  string   `json:"actual_agent,omitempty"`
	ExitCode     *int     `json:"exit_code,omitempty"`
	ErrorCode    string   `json:"error_code,omitempty"`
	Error        string   `json:"error,omitempty"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	PatchFile    string   `json:"patch_file,omitempty"`
	OutputTail   string   `json:"output_tail,omitempty"`
}

var jobResultCmd = &cobra.Command{
	Use:   "result <job_id>",
	Short: "Show result of a completed async job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]
		state, err := job.ReadState(jobID)
		if err != nil {
			return fmt.Errorf("read job %s: %w", jobID, err)
		}
		state = job.DetectLost(state)

		if !job.IsTerminal(state.Status) {
			return fmt.Errorf("job %s is still %s", jobID, state.Status)
		}

		outputTail := readOutputTail(state.OutputFile, jobResultOutputTail)

		if jobResultFormat == "json" {
			out := jobResultOutput{
				JobID:        state.JobID,
				Status:       state.Status,
				Agent:        state.Agent,
				ActualAgent:  state.ActualAgent,
				ExitCode:     state.ExitCode,
				ErrorCode:    state.ErrorCode,
				Error:        state.Error,
				ChangedFiles: state.ChangedFiles,
				PatchFile:    state.PatchFile,
				OutputTail:   outputTail,
			}
			data, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		// Text format
		fmt.Fprintf(os.Stdout, "Job:    %s\n", state.JobID)
		fmt.Fprintf(os.Stdout, "Status: %s\n", state.Status)
		if state.ErrorCode != "" {
			fmt.Fprintf(os.Stdout, "Error:  [%s] %s\n", state.ErrorCode, state.Error)
		}
		if len(state.ChangedFiles) > 0 {
			fmt.Fprintf(os.Stdout, "\nChanged files:\n")
			for _, f := range state.ChangedFiles {
				fmt.Fprintf(os.Stdout, "  %s\n", f)
			}
		}
		if state.PatchFile != "" {
			fmt.Fprintf(os.Stdout, "\nPatch: %s\n", state.PatchFile)
		}
		if outputTail != "" {
			fmt.Fprintf(os.Stdout, "\n--- output (last %d lines) ---\n%s", jobResultOutputTail, outputTail)
		}
		return nil
	},
}

// readOutputTail reads the last N lines from the output file.
func readOutputTail(path string, lines int) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return tailLines(string(data), lines)
}

// tailLines returns the last n lines of s.
func tailLines(s string, n int) string {
	if n <= 0 {
		return s
	}
	// Walk backwards counting newlines.
	count := 0
	i := len(s)
	for i > 0 {
		i--
		if s[i] == '\n' {
			count++
			if count == n {
				return s[i+1:]
			}
		}
	}
	return s
}

func init() {
	jobResultCmd.Flags().StringVar(&jobResultFormat, "format", "text", "output format: text or json")
	jobResultCmd.Flags().IntVar(&jobResultOutputTail, "output-tail-lines", 100, "number of output lines to include")
	jobCmd.AddCommand(jobResultCmd)
}
