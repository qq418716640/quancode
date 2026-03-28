package agent

import (
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
