package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qq418716640/quancode/config"
)

func TestInjectPromptFileRestoresOriginalContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	original := "# project guide\n"
	if err := os.WriteFile(path, []byte(original), 0640); err != nil {
		t.Fatalf("write original file: %v", err)
	}

	restore, err := injectPromptFile(path, "runtime prompt")
	if err != nil {
		t.Fatalf("injectPromptFile returned error: %v", err)
	}

	injected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read injected file: %v", err)
	}
	if string(injected) == original {
		t.Fatalf("expected injected content to differ from original")
	}

	if err := restore(); err != nil {
		t.Fatalf("restore returned error: %v", err)
	}

	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(restored) != original {
		t.Fatalf("expected original content after restore, got %q", string(restored))
	}
}

func TestInjectPromptFileRemovesCreatedFileOnRestore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	restore, err := injectPromptFile(path, "runtime prompt")
	if err != nil {
		t.Fatalf("injectPromptFile returned error: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected injected file to exist: %v", err)
	}

	if err := restore(); err != nil {
		t.Fatalf("restore returned error: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed after restore, got err=%v", err)
	}
}

func TestDelegateInjectsApprovalEnvAndCleansDir(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "env.txt")
	oldEnv := os.Getenv("QUANCODE_AGENT_TEST_PARENT")
	if err := os.Setenv("QUANCODE_AGENT_TEST_PARENT", "present"); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	defer os.Setenv("QUANCODE_AGENT_TEST_PARENT", oldEnv)
	a := FromConfig("test", config.AgentConfig{
		Command:      "/bin/sh",
		DelegateArgs: []string{"-c", "printf '%s\n%s\n%s\n' \"$QUANCODE_DELEGATION_ID\" \"$QUANCODE_APPROVAL_DIR\" \"$QUANCODE_AGENT_TEST_PARENT\" > env.txt"},
		Enabled:      true,
	})

	result, err := a.Delegate(dir, "ignored", DelegateOptions{})
	if err != nil {
		t.Fatalf("Delegate returned error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected result, got nil")
	}
	if !strings.HasPrefix(result.DelegationID, "del_") {
		t.Fatalf("expected delegation id prefix, got %q", result.DelegationID)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read env output: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines of env output, got %q", string(data))
	}
	if lines[0] != result.DelegationID {
		t.Fatalf("expected QUANCODE_DELEGATION_ID %q, got %q", result.DelegationID, lines[0])
	}
	if !strings.Contains(lines[1], "quancode-approval-"+result.DelegationID) {
		t.Fatalf("expected approval dir to include delegation id, got %q", lines[1])
	}
	if lines[2] != "present" {
		t.Fatalf("expected inherited parent env var, got %q", lines[2])
	}
	if _, err := os.Stat(lines[1]); !os.IsNotExist(err) {
		t.Fatalf("expected approval dir to be cleaned up, got err=%v", err)
	}
}

func TestNameReturnsConfigName(t *testing.T) {
	a := FromConfig("claude", config.AgentConfig{
		Name:    "Claude Code",
		Command: "claude",
		Enabled: true,
	})
	if a.Name() != "Claude Code" {
		t.Fatalf("expected name %q, got %q", "Claude Code", a.Name())
	}
}

func TestIsAvailableForRealCommand(t *testing.T) {
	a := FromConfig("sh", config.AgentConfig{
		Command: "sh",
		Enabled: true,
	})
	ok, path := a.IsAvailable()
	if !ok {
		t.Fatal("expected sh to be available")
	}
	if path == "" {
		t.Fatal("expected non-empty path for sh")
	}
}

func TestIsAvailableForMissingCommand(t *testing.T) {
	a := FromConfig("fake", config.AgentConfig{
		Command: "nonexistent-cmd-xyz-12345",
		Enabled: true,
	})
	ok, path := a.IsAvailable()
	if ok {
		t.Fatal("expected nonexistent command to be unavailable")
	}
	if path != "" {
		t.Fatal("expected empty path for unavailable command")
	}
}

func TestExitStatusErrorMessage(t *testing.T) {
	e := &ExitStatusError{Code: 42}
	if e.Error() != "process exited with status 42" {
		t.Fatalf("unexpected error message: %q", e.Error())
	}
}

func TestExitStatusErrorUnwrap(t *testing.T) {
	e := &ExitStatusError{Code: 7}
	var target *ExitStatusError
	if !errors.As(e, &target) {
		t.Fatal("expected errors.As to match ExitStatusError")
	}
	if target.Code != 7 {
		t.Fatalf("expected code 7, got %d", target.Code)
	}
}

func TestDelegateNoDelegateArgsReturnsError(t *testing.T) {
	a := FromConfig("bad", config.AgentConfig{
		Command:      "",
		DelegateArgs: nil,
		Enabled:      true,
	})
	_, err := a.Delegate(t.TempDir(), "task", DelegateOptions{})
	if err == nil {
		t.Fatal("expected error for agent with no delegate_args and no command")
	}
	if !strings.Contains(err.Error(), "no delegate_args configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDelegateDefaultTimeout(t *testing.T) {
	dir := t.TempDir()
	a := FromConfig("test", config.AgentConfig{
		Command:      "/bin/sh",
		DelegateArgs: []string{"-c", "echo ok"},
		Enabled:      true,
		TimeoutSecs:  0, // should default to 300
	})
	result, err := a.Delegate(dir, "task", DelegateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestDelegateWithStdinTaskMode(t *testing.T) {
	dir := t.TempDir()
	a := FromConfig("test", config.AgentConfig{
		Command:      "cat",
		DelegateArgs: []string{},
		Enabled:      true,
		TaskMode:     "stdin",
	})
	result, err := a.Delegate(dir, "hello via stdin", DelegateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "hello via stdin" {
		t.Fatalf("expected stdin task in output, got %q", result.Stdout)
	}
}

func TestDelegateWithExplicitDelegationID(t *testing.T) {
	dir := t.TempDir()
	a := FromConfig("test", config.AgentConfig{
		Command:      "/bin/sh",
		DelegateArgs: []string{"-c", "echo ok"},
		Enabled:      true,
	})
	result, err := a.Delegate(dir, "task", DelegateOptions{
		DelegationID: "del_custom123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DelegationID != "del_custom123" {
		t.Fatalf("expected custom delegation ID, got %q", result.DelegationID)
	}
}

func TestDelegateWithCustomEnv(t *testing.T) {
	dir := t.TempDir()
	a := FromConfig("test", config.AgentConfig{
		Command:      "/bin/sh",
		DelegateArgs: []string{"-c", "printf $MY_TEST_VAR"},
		Enabled:      true,
		Env:          map[string]string{"MY_TEST_VAR": "custom_value"},
	})
	result, err := a.Delegate(dir, "task", DelegateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "custom_value" {
		t.Fatalf("expected custom env var in output, got %q", result.Stdout)
	}
}

func TestCleanPromptFileRemovesInjectedContent(t *testing.T) {
	original := "# original content\n"
	injected := original + fileInjectBegin + "injected prompt" + fileInjectEnd
	got := cleanPromptFile(injected)
	if got != original {
		t.Fatalf("expected %q, got %q", original, got)
	}
}

func TestCleanPromptFileNoMarkers(t *testing.T) {
	content := "no markers here\n"
	got := cleanPromptFile(content)
	if got != content {
		t.Fatalf("expected unchanged content, got %q", got)
	}
}

func TestCleanPromptFileMalformedMarker(t *testing.T) {
	content := "before" + fileInjectBegin + "no end marker"
	got := cleanPromptFile(content)
	if got != "before" {
		t.Fatalf("expected content before malformed marker, got %q", got)
	}
}

func TestCleanPromptFileMultipleInjections(t *testing.T) {
	content := "a" + fileInjectBegin + "first" + fileInjectEnd + "b" + fileInjectBegin + "second" + fileInjectEnd + "c"
	got := cleanPromptFile(content)
	if got != "abc" {
		t.Fatalf("expected 'abc' after cleaning multiple injections, got %q", got)
	}
}

func TestRunManagedPrimaryExitCode(t *testing.T) {
	err := runManagedPrimary("/bin/sh", []string{"sh", "-c", "exit 3"}, os.Environ(), t.TempDir())
	var exitErr *ExitStatusError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitStatusError, got %v", err)
	}
	if exitErr.Code != 3 {
		t.Fatalf("expected exit code 3, got %d", exitErr.Code)
	}
}

func TestRunManagedPrimarySuccess(t *testing.T) {
	err := runManagedPrimary("/bin/sh", []string{"sh", "-c", "true"}, os.Environ(), t.TempDir())
	if err != nil {
		t.Fatalf("expected nil error for success, got %v", err)
	}
}
