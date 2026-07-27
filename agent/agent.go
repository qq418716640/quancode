package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

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
		"QUANCODE_SESSION":     "1",
		"QUANCODE_PRIMARY":     a.key,
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

// sanitizeTaskUTF8 ensures task text is valid UTF-8 before passing to a CLI.
// Returns the (possibly sanitized) task string.
func sanitizeTaskUTF8(task string) string {
	if utf8.ValidString(task) {
		return task
	}
	fmt.Fprintf(os.Stderr, "[quancode] warning: task text contained invalid UTF-8 (sanitized)\n")
	return strings.ToValidUTF8(task, "\uFFFD")
}

func (a *genericAgent) Delegate(workDir, task string, opts DelegateOptions) (result *runner.Result, err error) {
	var args, env []string
	var timeout int
	var delegationID string
	args, env, timeout, delegationID, err = a.delegatePrep(opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		if result != nil {
			result.DelegationID = delegationID
		}
		a.applyResultFormat(result)
		applyDeprecationNotices(result)
		a.applyDiagnosticHints(result, err)
	}()

	task = sanitizeTaskUTF8(task)

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
			return runner.RunWithOutputFile(workDir, timeout, env, a.cfg.OutputFlag, a.cfg.Command, args, "")
		}
		return runner.RunWithStdin(workDir, timeout, env, task, a.cfg.Command, args...)
	}

	// taskMode == "arg"
	if outputMode == "file" && a.cfg.OutputFlag != "" {
		return runner.RunWithOutputFile(workDir, timeout, env, a.cfg.OutputFlag, a.cfg.Command, args, task)
	}

	args = append(args, task)
	return runner.Run(workDir, timeout, env, a.cfg.Command, args...)
}

func (a *genericAgent) DelegateWithContext(ctx context.Context, workDir, task string, opts DelegateOptions) (result *runner.Result, err error) {
	var args, env []string
	var timeout int
	var delegationID string
	args, env, timeout, delegationID, err = a.delegatePrep(opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		if result != nil {
			result.DelegationID = delegationID
		}
		a.applyResultFormat(result)
		applyDeprecationNotices(result)
		a.applyDiagnosticHints(result, err)
	}()

	task = sanitizeTaskUTF8(task)

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
			return runner.RunWithOutputFileContext(ctx, workDir, env, a.cfg.OutputFlag, a.cfg.Command, args, "")
		}
		return runner.RunWithStdinContext(ctx, workDir, env, task, a.cfg.Command, args...)
	}

	// taskMode == "arg"
	if outputMode == "file" && a.cfg.OutputFlag != "" {
		return runner.RunWithOutputFileContext(ctx, workDir, env, a.cfg.OutputFlag, a.cfg.Command, args, task)
	}

	args = append(args, task)
	return runner.RunWithContext(ctx, workDir, env, a.cfg.Command, args...)
}

// claudeJSONResult is the schema of `claude -p --output-format json`'s single
// stdout object, restricted to the fields QuanCode reads. Verified empirically
// against claude-code 2.1.219, including the error path (an invalid --model
// still yields a full object with is_error/result/api_error_status set).
type claudeJSONResult struct {
	Result         *string  `json:"result"`
	TotalCostUSD   *float64 `json:"total_cost_usd"`
	SessionID      *string  `json:"session_id"`
	APIErrorStatus *int     `json:"api_error_status"`
	Usage          *struct {
		// InputTokens counts only the uncached portion of the prompt and is
		// routinely a single-digit number on a cached turn (verified: a real
		// turn reported input_tokens=2 against 15273 cache-read + 6977
		// cache-write). Recording it alone would be actively misleading, so
		// TokensIn is the sum of all three — every token the model read.
		InputTokens             *int64 `json:"input_tokens"`
		CacheCreationInputToken *int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens    *int64 `json:"cache_read_input_tokens"`
		OutputTokens            *int64 `json:"output_tokens"`
	} `json:"usage"`
}

// codexJSONLEvent is one line of `codex exec --json`'s JSONL event stream,
// restricted to the two event shapes QuanCode reads. Verified empirically
// against codex-cli 0.145.0.
// Codex reports no dollar cost (the subscription bills the account, not the
// call), so CostUSD stays nil for it — which is exactly what the pointer is
// for: "not reported" is not "$0".
type codexJSONLEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Item     *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
	// Unlike claude's, codex's input_tokens is the full prompt size, with
	// cached_input_tokens reported separately as a subset. No summing.
	Usage *struct {
		InputTokens  *int64 `json:"input_tokens"`
		OutputTokens *int64 `json:"output_tokens"`
	} `json:"usage"`
}

// httpStatusRateLimited is the HTTP status Claude's JSON envelope reports for
// a 429; used to mark the failure transient/agent-fault directly rather than
// relying on text-pattern matching, which is what config.CommonFailurePatterns
// falls back to for every other agent.
const httpStatusRateLimited = 429

// rateLimitHint must stay identical to the hint config.CommonFailurePatterns
// attaches to "too many requests" so the two detection paths collapse to one
// message when both fire on the same failure.
const rateLimitHint = "Agent returned HTTP 429. Falling back to another agent."

// applyResultFormat interprets result.Stdout according to the agent's
// configured ResultFormat, replacing it with the human-readable answer and
// populating cost/token metadata. Runs before applyDiagnosticHints so hint
// matching sees the unwrapped text, not a JSON envelope.
//
// Applies on every completed process, success or failure alike — a failed
// turn can still have consumed tokens and money, which is the case most
// worth surfacing. Output that does not parse (empty, unexpected shape, a
// CLI that ignored the flag) is a silent no-op: Stdout is left exactly as it
// was and no metadata is set. A stream truncated mid-turn is the one partial
// case — whatever the agent managed to emit is kept, unreported counts stay
// nil, and the pre-parse text is preserved in RawStdout so hint matching
// still sees the failure evidence.
func (a *genericAgent) applyResultFormat(result *runner.Result) {
	if result == nil {
		return
	}
	switch a.cfg.ResultFormat {
	case "json_object":
		applyClaudeJSONResult(result)
	case "jsonl_events":
		applyCodexJSONLEvents(result)
	}
}

func applyClaudeJSONResult(result *runner.Result) {
	var r claudeJSONResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &r); err != nil {
		return
	}
	// A legitimate envelope always has at least one of these; an empty
	// decode (e.g. Stdout was "{}" or unrelated JSON) is not one.
	if r.Result == nil && r.TotalCostUSD == nil && r.Usage == nil && r.SessionID == nil && r.APIErrorStatus == nil {
		return
	}
	// An empty .result carries no answer; replacing Stdout with it would
	// throw the envelope away and leave the delegation with no output at all.
	if r.Result != nil && *r.Result != "" {
		result.RawStdout = result.Stdout
		result.Stdout = *r.Result
	}
	// Otherwise leave Stdout as the raw envelope rather than blanking it —
	// still readable, and better than losing the only text QuanCode captured.
	if r.TotalCostUSD != nil {
		result.CostUSD = r.TotalCostUSD
	}
	if r.SessionID != nil {
		result.AgentSessionID = *r.SessionID
	}
	if r.Usage != nil {
		if in := sumTokens(r.Usage.InputTokens, r.Usage.CacheCreationInputToken, r.Usage.CacheReadInputTokens); in != nil {
			result.TokensIn = in
		}
		result.TokensOut = r.Usage.OutputTokens
	}
	if r.APIErrorStatus != nil && *r.APIErrorStatus == httpStatusRateLimited {
		result.Transient = true
		result.AgentFault = true
		// Deliberately the same sentence config.CommonFailurePatterns uses
		// for "too many requests": an envelope that carries both the status
		// code and the text must not print the same diagnosis twice.
		// applyDiagnosticHints does the printing and the dedupe.
		result.MatchedHints = append(result.MatchedHints, rateLimitHint)
	}
}

// sumTokens adds the non-nil counts, returning nil if all of them are absent
// so an entirely missing usage block stays "not reported" rather than zero.
func sumTokens(counts ...*int64) *int64 {
	var total int64
	seen := false
	for _, c := range counts {
		if c != nil {
			total += *c
			seen = true
		}
	}
	if !seen {
		return nil
	}
	return &total
}

func applyCodexJSONLEvents(result *runner.Result) {
	var finalText, threadID *string
	var tokensIn, tokensOut *int64

	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev codexJSONLEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "thread.started":
			if ev.ThreadID != "" {
				id := ev.ThreadID
				threadID = &id
			}
		case "item.completed":
			if ev.Item != nil && ev.Item.Type == "agent_message" {
				text := ev.Item.Text
				finalText = &text
			}
		case "turn.completed":
			if ev.Usage != nil {
				tokensIn = ev.Usage.InputTokens
				tokensOut = ev.Usage.OutputTokens
			}
		}
	}

	if finalText != nil {
		result.RawStdout = result.Stdout
		result.Stdout = *finalText
	}
	if threadID != nil {
		result.AgentSessionID = *threadID
	}
	if tokensIn != nil {
		result.TokensIn = tokensIn
	}
	if tokensOut != nil {
		result.TokensOut = tokensOut
	}
}

// applyDeprecationNotices records lifecycle warnings from the agent's
// stderr — a CLI announcing that a flag QuanCode passes is on its way out.
//
// Unlike applyDiagnosticHints this runs regardless of exit code, because a
// deprecation warning is what a *successful* run looks like right before it
// stops being successful. It is also why the warning is so easy to miss:
// runner captures stderr into a buffer that nothing reads on the success
// path, so these never reach a terminal on their own. Recording them is the
// entire point.
//
// Advisory only. Nothing here touches Transient, AgentFault, or
// MatchedHints: a deprecation is not a failure, and folding it into the
// hint list would corrupt both health analysis and the meaning of that
// field in the ledger.
func applyDeprecationNotices(result *runner.Result) {
	if result == nil {
		return
	}
	result.Deprecations = config.MatchDeprecations(result.Stderr)
}

// applyDiagnosticHints scans the combined stderr+stdout of a failed
// delegation against the agent's configured hints and prints matching
// recovery messages to stderr. Called from Delegate/DelegateWithContext
// defers so all code paths are covered.
//
// Only scans on failure signals (non-zero exit, timeout, cancellation, or
// launch error). Successful runs produce no hints.
func (a *genericAgent) applyDiagnosticHints(result *runner.Result, runErr error) {
	failed := runErr != nil || (result != nil && (result.ExitCode != 0 || result.TimedOut || result.Cancelled))
	if !failed || result == nil {
		return
	}
	// RawStdout is included so unwrapping a structured envelope cannot hide
	// the error text a pattern needs to see; it is empty unless a parser
	// rewrote Stdout, so the common path scans exactly what it always did.
	combined := result.Stderr + "\n" + result.Stdout + "\n" + result.RawStdout
	m := config.MatchFailure(combined, a.cfg.DiagnosticHints)

	// Hints already on the Result were attached by applyResultFormat from a
	// structured envelope and have not been printed yet. Printing happens
	// here for both sources so a failure detected two ways — an api_error
	// status code and the matching error text — is diagnosed once.
	var hints []string
	seen := map[string]bool{}
	for _, h := range append(append([]string{}, result.MatchedHints...), m.Hints...) {
		if seen[h] {
			continue
		}
		seen[h] = true
		hints = append(hints, h)
		fmt.Fprintf(os.Stderr, "[quancode hint] %s\n", h)
	}
	result.MatchedHints = hints
	if m.Transient {
		result.Transient = true
	}
	if m.AgentFault {
		result.AgentFault = true
	}
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
