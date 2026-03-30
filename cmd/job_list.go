package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/qq418716640/quancode/job"
	"github.com/spf13/cobra"
)

var (
	jobListWorkDir string
	jobListLimit   int
	jobListFormat  string
	jobListLatest  bool
)

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List async jobs",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		workDir := jobListWorkDir
		limit := jobListLimit
		if jobListLatest {
			limit = 1
		}

		jobs, err := job.ListJobs(workDir, limit)
		if err != nil {
			return err
		}

		if len(jobs) == 0 {
			if workDir != "" {
				fmt.Fprintf(os.Stderr, "no jobs found for %s\n", workDir)
			} else {
				fmt.Fprintln(os.Stderr, "no jobs found")
			}
			return nil
		}

		if jobListFormat == "json" {
			data, _ := json.MarshalIndent(jobs, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "JOB_ID\tSTATUS\tAGENT\tTASK\tDURATION\tCREATED")
		for _, s := range jobs {
			taskPreview := s.Task
			runes := []rune(taskPreview)
			if len(runes) > 60 {
				taskPreview = string(runes[:57]) + "..."
			}
			duration := "-"
			if s.FinishedAt != "" {
				if created, err1 := time.Parse(time.RFC3339, s.CreatedAt); err1 == nil {
					if finished, err2 := time.Parse(time.RFC3339, s.FinishedAt); err2 == nil {
						duration = finished.Sub(created).Round(time.Second).String()
					}
				}
			}
			created := formatTimeShort(s.CreatedAt)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				s.JobID, s.Status, agentDisplay(s), taskPreview, duration, created)
		}
		w.Flush()
		return nil
	},
}

// agentDisplay shows actual_agent if it differs from agent (fallback occurred).
func agentDisplay(s *job.State) string {
	if s.ActualAgent != "" && s.ActualAgent != s.Agent {
		return s.ActualAgent + " (←" + s.Agent + ")"
	}
	return s.Agent
}

func init() {
	jobListCmd.Flags().StringVar(&jobListWorkDir, "workdir", "", "filter by working directory")
	jobListCmd.Flags().IntVar(&jobListLimit, "limit", 50, "max number of jobs to show")
	jobListCmd.Flags().StringVar(&jobListFormat, "format", "text", "output format: text or json")
	jobListCmd.Flags().BoolVar(&jobListLatest, "latest", false, "show only the most recent job")
	jobCmd.AddCommand(jobListCmd)
}
