package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/qq418716640/quancode/config"
	"github.com/qq418716640/quancode/ledger"
	"github.com/qq418716640/quancode/runner"
)

const (
	fileInjectBegin = "\n\n<!-- quancode:begin -->\n"
	fileInjectEnd   = "\n<!-- quancode:end -->\n"
)

type Agent interface {
	Name() string
	LaunchAsPrimary(workDir, systemPrompt string) error
	Delegate(workDir, task string, opts DelegateOptions) (*runner.Result, error)
	// DelegateWithContext is like Delegate but uses the provided context for cancellation.
	// Used by speculative parallelism to cancel a running agent.
	DelegateWithContext(ctx context.Context, workDir, task string, opts DelegateOptions) (*runner.Result, error)
	IsAvailable() (bool, string)
}

type DelegateOptions struct {
	DelegationID    string
	TimeoutOverride int // per-task timeout in seconds; 0 means use agent default
	MinTimeout      int // floor for effective timeout; 0 means no floor
}

type ExitStatusError struct {
	Code int
}

func (e *ExitStatusError) Error() string {
	return fmt.Sprintf("process exited with status %d", e.Code)
}

// FromConfig creates an Agent from config. All CLIs use the same
// data-driven genericAgent — no per-CLI Go code needed.
func FromConfig(key string, cfg config.AgentConfig) Agent {
	return &genericAgent{key: key, cfg: cfg}
}

// genericAgent is a data-driven implementation that works for any CLI
// based on config fields (DelegateArgs, TaskMode, OutputMode, PromptMode, etc.).
type genericAgent struct {
	key string
	cfg config.AgentConfig
}

func (a *genericAgent) Name() string {
	return a.cfg.Name
}

func (a *genericAgent) LaunchAsPrimary(workDir, systemPrompt string) error {
	binary, err := exec.LookPath(a.cfg.Command)
	if err != nil {
		return fmt.Errorf("agent %q: command %q not found", a.key, a.cfg.Command)
	}

	promptMode := a.cfg.PromptMode
	if promptMode == "" {
		promptMode = "append_arg"
	}

	cliArgs := []string{a.cfg.Command}
	cliArgs = append(cliArgs, a.cfg.PrimaryArgs...)

	env := runner.MergeEnv(os.Environ(), a.cfg.Env)
	env = runner.MergeEnv(env, map[string]string{
		"QUANCODE_SESSION":    "1",
		"QUANCODE_PRIMARY":    a.key,
		"QUANCODE_PROMPT_MODE": promptMode,
	})

	switch promptMode {
	case "append_arg":
		cliArgs = append(cliArgs, systemPrompt)
	case "env":
		env = append(env, "QUANCODE_SYSTEM_PROMPT="+systemPrompt)
	case "file":
		fileName := a.cfg.PromptFile
		if fileName == "" {
			fileName = "AGENTS.md"
		}
		filePath := filepath.Join(workDir, fileName)
		restore, err := injectPromptFile(filePath, systemPrompt)
		if err != nil {
			return fmt.Errorf("inject prompt file: %w", err)
		}
		defer func() {
			if restoreErr := restore(); restoreErr != nil {
				fmt.Fprintf(os.Stderr, "[quancode] failed to restore %s: %v\n", fileName, restoreErr)
			}
		}()

		fmt.Fprintf(os.Stderr, "[quancode] injected delegation instructions into %s\n", fileName)
		return runManagedPrimary(binary, cliArgs, env, workDir)
	case "stdin":
		return fmt.Errorf("agent %q: prompt_mode 'stdin' not supported for primary launch", a.key)
	}

	return syscall.Exec(binary, cliArgs, env)
}

// delegatePrep prepares the common delegation state: args, env, timeout, delegationID.
func (a *genericAgent) delegatePrep(opts DelegateOptions) (args []string, env []string, timeout int, delegationID string, err error) {
	if len(a.cfg.DelegateArgs) == 0 && a.cfg.Command == "" {
		return nil, nil, 0, "", fmt.Errorf("agent %q: no delegate_args configured", a.key)
	}

	args = make([]string, len(a.cfg.DelegateArgs))
	copy(args, a.cfg.DelegateArgs)

	timeout = a.cfg.TimeoutSecs
	if timeout <= 0 {
		timeout = 300
	}
	if opts.TimeoutOverride > 0 && opts.TimeoutOverride < timeout {
		timeout = opts.TimeoutOverride
	}
	if opts.MinTimeout > 0 && timeout < opts.MinTimeout {
		fmt.Fprintf(os.Stderr, "[quancode] effective timeout %ds raised to min_timeout_secs %ds\n", timeout, opts.MinTimeout)
		timeout = opts.MinTimeout
	}

	env = runner.BuildEnv(a.cfg.Env)
	if env == nil {
		env = os.Environ()
	}
	delegationID = opts.DelegationID
	if delegationID == "" {
		delegationID, err = ledger.NewDelegationID()
		if err != nil {
			return nil, nil, 0, "", fmt.Errorf("generate delegation id: %w", err)
		}
	}
	env = runner.MergeEnv(env, map[string]string{
		"QUANCODE_DELEGATION_ID": delegationID,
	})

	return args, env, timeout, delegationID, nil
}

func (a *genericAgent) Delegate(workDir, task string, opts DelegateOptions) (*runner.Result, error) {
	args, env, timeout, delegationID, err := a.delegatePrep(opts)
	if err != nil {
		return nil, err
	}

	taskMode := a.cfg.TaskMode
	if taskMode == "" {
		taskMode = "arg"
	}

	outputMode := a.cfg.OutputMode
	if outputMode == "" {
		outputMode = "stdout"
	}

	if taskMode == "stdin" {
		if outputMode == "file" && a.cfg.OutputFlag != "" {
			result, err := runner.RunWithOutputFile(workDir, timeout, env, a.cfg.OutputFlag, a.cfg.Command, args, "")
			if result != nil {
				result.DelegationID = delegationID
			}
			return result, err
		}
		result, err := runner.RunWithStdin(workDir, timeout, env, task, a.cfg.Command, args...)
		if result != nil {
			result.DelegationID = delegationID
		}
		return result, err
	}

	// taskMode == "arg"
	if outputMode == "file" && a.cfg.OutputFlag != "" {
		result, err := runner.RunWithOutputFile(workDir, timeout, env, a.cfg.OutputFlag, a.cfg.Command, args, task)
		if result != nil {
			result.DelegationID = delegationID
		}
		return result, err
	}

	args = append(args, task)

	result, err := runner.Run(workDir, timeout, env, a.cfg.Command, args...)
	if result != nil {
		result.DelegationID = delegationID
	}
	return result, err
}

func (a *genericAgent) DelegateWithContext(ctx context.Context, workDir, task string, opts DelegateOptions) (*runner.Result, error) {
	args, env, timeout, delegationID, err := a.delegatePrep(opts)
	if err != nil {
		return nil, err
	}

	// Safety net: if the caller's context has no deadline, apply the agent's
	// own timeout to prevent infinite execution.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	taskMode := a.cfg.TaskMode
	if taskMode == "" {
		taskMode = "arg"
	}

	outputMode := a.cfg.OutputMode
	if outputMode == "" {
		outputMode = "stdout"
	}

	if taskMode == "stdin" {
		if outputMode == "file" && a.cfg.OutputFlag != "" {
			result, err := runner.RunWithOutputFileContext(ctx, workDir, env, a.cfg.OutputFlag, a.cfg.Command, args, "")
			if result != nil {
				result.DelegationID = delegationID
			}
			return result, err
		}
		result, err := runner.RunWithStdinContext(ctx, workDir, env, task, a.cfg.Command, args...)
		if result != nil {
			result.DelegationID = delegationID
		}
		return result, err
	}

	// taskMode == "arg"
	if outputMode == "file" && a.cfg.OutputFlag != "" {
		result, err := runner.RunWithOutputFileContext(ctx, workDir, env, a.cfg.OutputFlag, a.cfg.Command, args, task)
		if result != nil {
			result.DelegationID = delegationID
		}
		return result, err
	}

	args = append(args, task)

	result, err := runner.RunWithContext(ctx, workDir, env, a.cfg.Command, args...)
	if result != nil {
		result.DelegationID = delegationID
	}
	return result, err
}

func (a *genericAgent) IsAvailable() (bool, string) {
	path, err := exec.LookPath(a.cfg.Command)
	if err != nil {
		return false, ""
	}
	return true, path
}

func runManagedPrimary(binary string, args, env []string, workDir string) error {
	cmd := exec.Command(binary, args[1:]...)
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer func() {
		signal.Stop(sigCh)
		close(sigCh)
	}()

	go func() {
		for sig := range sigCh {
			if cmd.Process != nil {
				if s, ok := sig.(syscall.Signal); ok {
					_ = runner.KillProcessGroup(cmd.Process.Pid, s)
				} else {
					_ = cmd.Process.Signal(sig)
				}
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				if status.Exited() {
					return &ExitStatusError{Code: status.ExitStatus()}
				}
				if status.Signaled() {
					return &ExitStatusError{Code: 128 + int(status.Signal())}
				}
			}
			if code := exitErr.ExitCode(); code >= 0 {
				return &ExitStatusError{Code: code}
			}
		}
		return err
	}
	return nil
}

// injectPromptFile appends the system prompt to the target file between markers
// and returns a restore function that puts the file back after the primary exits.
func injectPromptFile(filePath, prompt string) (func() error, error) {
	original, readErr := os.ReadFile(filePath)
	existed := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, readErr
	}

	mode := os.FileMode(0644)
	if existed {
		if info, err := os.Stat(filePath); err == nil {
			mode = info.Mode().Perm()
		}
	}

	cleanOriginal := cleanPromptFile(string(original))
	content := cleanOriginal + fileInjectBegin + prompt + fileInjectEnd

	if err := os.WriteFile(filePath, []byte(content), mode); err != nil {
		return nil, err
	}

	restore := func() error {
		if !existed {
			err := os.Remove(filePath)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		}

		return os.WriteFile(filePath, []byte(cleanOriginal), mode)
	}

	return restore, nil
}

// cleanPromptFile removes quancode-injected content from a file's content.
func cleanPromptFile(content string) string {
	for {
		beginIdx := strings.Index(content, fileInjectBegin)
		if beginIdx < 0 {
			break
		}
		endIdx := strings.Index(content[beginIdx:], fileInjectEnd)
		if endIdx < 0 {
			// Malformed marker, remove from begin to end of file
			content = content[:beginIdx]
			break
		}
		content = content[:beginIdx] + content[beginIdx+endIdx+len(fileInjectEnd):]
	}
	return content
}
