package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type Result struct {
	DelegationID string
	Stdout       string
	Stderr       string
	ExitCode     int
	DurationMs   int64
	// TimedOut is set when the context hit its deadline.
	// Cancelled is set when the context was cancelled by the caller
	// (e.g. speculative execution cancelling the losing agent).
	// The two are mutually exclusive.
	TimedOut  bool
	Cancelled bool
}

// MergeEnv replaces env vars in base with values from extra (case-insensitive key match).
func MergeEnv(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return base
	}

	overrides := make(map[string]string, len(extra))
	for k, v := range extra {
		overrides[strings.ToUpper(k)] = k + "=" + v
	}

	result := make([]string, 0, len(base)+len(extra))
	for _, entry := range base {
		key := entry
		if idx := strings.IndexByte(key, '='); idx >= 0 {
			key = strings.ToUpper(key[:idx])
		}
		if override, ok := overrides[key]; ok {
			result = append(result, override)
			delete(overrides, key)
		} else {
			result = append(result, entry)
		}
	}

	for _, v := range overrides {
		result = append(result, v)
	}

	return result
}

// BuildEnv merges extra env vars into the current environment.
// Returns nil when extra is empty (meaning inherit parent env).
func BuildEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return nil
	}
	return MergeEnv(os.Environ(), extra)
}

// KillProcessGroup sends a signal to the entire process group of the given PID.
// Falls back to signalling just the process if the group kill fails.
func KillProcessGroup(pid int, sig syscall.Signal) error {
	pgid, err := syscall.Getpgid(pid)
	if err == nil {
		if killErr := syscall.Kill(-pgid, sig); killErr == nil {
			return nil
		}
	}
	return syscall.Kill(pid, sig)
}

// killProcessGroup sends a signal to the entire process group via exec.Cmd.
// Falls back to killing just the process if group kill fails.
func killProcessGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	return KillProcessGroup(cmd.Process.Pid, sig)
}

// setupProcessGroup configures the command to run in its own process group
// and sets up Cancel/WaitDelay so the entire group is killed on context cancel.
func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return killProcessGroup(cmd, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second
}

// runCmd is the shared execution core for all Run variants.
func runCmd(ctx context.Context, cmd *exec.Cmd) (*Result, error) {
	setupProcessGroup(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	durationMs := time.Since(start).Milliseconds()

	result := &Result{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMs: durationMs,
	}

	if ctx.Err() != nil {
		result.ExitCode = 124
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.TimedOut = true
			return result, fmt.Errorf("command timed out")
		}
		result.Cancelled = true
		return result, fmt.Errorf("command cancelled")
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}

	if err != nil {
		return result, err
	}

	return result, nil
}

// Run executes a command with timeout and captures output.
// If env is non-nil, it replaces the subprocess environment.
func Run(workDir string, timeoutSecs int, env []string, name string, args ...string) (*Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	cmd.Env = env

	return runCmd(ctx, cmd)
}

// RunWithContext executes a command using the provided context.
// The caller controls cancellation (used by speculative parallelism).
func RunWithContext(ctx context.Context, workDir string, env []string, name string, args ...string) (*Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	cmd.Env = env

	return runCmd(ctx, cmd)
}

// RunWithStdin executes a command and pipes stdinContent to its stdin.
func RunWithStdin(workDir string, timeoutSecs int, env []string, stdinContent string, name string, args ...string) (*Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdin = bytes.NewReader([]byte(stdinContent))

	return runCmd(ctx, cmd)
}

// RunWithStdinContext executes a command with stdin using the provided context.
func RunWithStdinContext(ctx context.Context, workDir string, env []string, stdinContent string, name string, args ...string) (*Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdin = bytes.NewReader([]byte(stdinContent))

	return runCmd(ctx, cmd)
}

// runWithOutputFileCore contains shared logic for output file variants.
func runWithOutputFileCore(runFn func(name string, args ...string) (*Result, error), outputFlag string, name string, flagArgs []string, prompt string) (*Result, error) {
	tmpFile, err := os.CreateTemp("", "quancode-output-*.txt")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Build args: flags... --output-last-message tmpfile "prompt"
	var fullArgs []string
	fullArgs = append(fullArgs, flagArgs...)
	fullArgs = append(fullArgs, outputFlag, tmpPath)
	fullArgs = append(fullArgs, prompt)

	result, err := runFn(name, fullArgs...)
	if err != nil {
		return result, err
	}

	// Read the output file
	data, readErr := os.ReadFile(tmpPath)
	if readErr == nil && len(data) > 0 {
		result.Stdout = string(data)
	}

	return result, nil
}

// RunWithOutputFile executes a command that writes output to a temp file.
// flagArgs are placed before the prompt, outputFlag and tmpfile are inserted between them.
// Final order: name flagArgs... outputFlag tmpfile prompt
func RunWithOutputFile(workDir string, timeoutSecs int, env []string, outputFlag string, name string, flagArgs []string, prompt string) (*Result, error) {
	return runWithOutputFileCore(func(n string, a ...string) (*Result, error) {
		return Run(workDir, timeoutSecs, env, n, a...)
	}, outputFlag, name, flagArgs, prompt)
}

// RunWithOutputFileContext executes a command that writes output to a temp file,
// using the provided context for cancellation.
func RunWithOutputFileContext(ctx context.Context, workDir string, env []string, outputFlag string, name string, flagArgs []string, prompt string) (*Result, error) {
	return runWithOutputFileCore(func(n string, a ...string) (*Result, error) {
		return RunWithContext(ctx, workDir, env, n, a...)
	}, outputFlag, name, flagArgs, prompt)
}
