package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/qq418716640/quancode/job"
)

// asyncResult is the JSON output for delegate --async.
type asyncResult struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

// launchAsyncJob creates a pending job and spawns a detached job-runner process.
// Returns immediately after the runner is started.
func launchAsyncJob(agentKey, task, workDir, isolation, contextDiff string, timeoutSecs int) error {
	jobID, err := job.NewJobID()
	if err != nil {
		return fmt.Errorf("generate job id: %w", err)
	}

	outputFile := job.OutputPath(jobID)

	state := &job.State{
		JobID:            jobID,
		Agent:            agentKey,
		Task:             task,
		WorkDir:          workDir,
		Isolation:        isolation,
		ContextDiff:      contextDiff,
		EffectiveTimeout: timeoutSecs,
		Status:           job.StatusPending,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
		OutputFile:       outputFile,
	}

	// Write pending state before spawning — if this fails, no orphan process.
	if err := job.WriteState(state); err != nil {
		return fmt.Errorf("write pending job state: %w", err)
	}

	// Resolve quancode binary path for the subprocess.
	quancodeBin, err := os.Executable()
	if err != nil {
		return failAsync(state, job.ErrCodeSpawnFailed, "resolve executable", err)
	}

	// Build args for the hidden _run-job subcommand.
	runArgs := []string{quancodeBin, "_run-job", "--job-id", jobID}
	if cfgFile != "" {
		runArgs = append(runArgs, "--config", cfgFile)
	}

	// Open a log file for the runner's own stdout/stderr (panics, init errors).
	logPath := filepath.Join(job.JobsDir(), jobID+".runner.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return failAsync(state, job.ErrCodeSpawnFailed, "open runner log", err)
	}
	defer logFile.Close()

	// Open /dev/null for stdin — detached process must not read from terminal.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return failAsync(state, job.ErrCodeSpawnFailed, "open /dev/null", err)
	}
	defer devNull.Close()

	attr := &os.ProcAttr{
		Dir:   workDir,
		Env:   os.Environ(),
		Files: []*os.File{devNull, logFile, logFile},
		Sys: &syscall.SysProcAttr{
			Setsid: true,
		},
	}

	proc, err := os.StartProcess(quancodeBin, runArgs, attr)
	if err != nil {
		return failAsync(state, job.ErrCodeSpawnFailed, "start job-runner", err)
	}

	// Release the process so the parent doesn't wait for it.
	proc.Release()

	// Output result.
	if delegateFormat == "json" {
		data, _ := json.MarshalIndent(asyncResult{
			JobID:  jobID,
			Status: job.StatusPending,
		}, "", "  ")
		fmt.Println(string(data))
	} else {
		fmt.Fprintf(os.Stderr, "[quancode] async job submitted: %s\n", jobID)
		fmt.Println(jobID)
	}

	return nil
}

// failAsync marks a job as failed and returns the error.
// Logs a warning if the state write itself fails.
func failAsync(state *job.State, errCode, context string, err error) error {
	if markErr := markFailed(state, errCode, fmt.Sprintf("%s: %v", context, err)); markErr != nil {
		fmt.Fprintf(os.Stderr, "[quancode] warning: could not mark job %s as failed: %v\n", state.JobID, markErr)
	}
	return fmt.Errorf("%s: %w", context, err)
}

// resolveEffectiveTimeout returns the effective timeout given the user flag,
// agent config, and global minimum. It takes the minimum of flag and config,
// then enforces the minTimeout floor. The second return value indicates
// whether the floor was applied (so callers can emit a warning).
func resolveEffectiveTimeout(flagTimeout, configTimeout, minTimeout int) (int, bool) {
	if configTimeout <= 0 {
		configTimeout = 1800 // default hard cap
	}
	effective := configTimeout
	if flagTimeout > 0 && flagTimeout < configTimeout {
		effective = flagTimeout
	}
	if minTimeout > 0 && effective < minTimeout {
		return minTimeout, true
	}
	return effective, false
}

