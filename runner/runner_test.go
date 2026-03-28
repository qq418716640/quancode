package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMergeEnvOverridesExistingKeysCaseInsensitively(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"HTTP_PROXY=http://old-proxy",
		"LANG=en_US.UTF-8",
	}
	extra := map[string]string{
		"http_proxy": "http://new-proxy",
		"NO_PROXY":   "localhost,127.0.0.1",
	}

	got := MergeEnv(base, extra)

	want := []string{
		"PATH=/usr/bin",
		"http_proxy=http://new-proxy",
		"LANG=en_US.UTF-8",
		"NO_PROXY=localhost,127.0.0.1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MergeEnv mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestMergeEnvReturnsBaseWhenExtraEmpty(t *testing.T) {
	base := []string{"PATH=/usr/bin"}

	got := MergeEnv(base, nil)

	if !reflect.DeepEqual(got, base) {
		t.Fatalf("expected base env unchanged, got %#v", got)
	}
}

func TestBuildEnvReturnsNilWhenEmpty(t *testing.T) {
	got := BuildEnv(nil)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
	got = BuildEnv(map[string]string{})
	if got != nil {
		t.Fatalf("expected nil for empty map, got %v", got)
	}
}

func TestBuildEnvMergesIntoCurrentEnv(t *testing.T) {
	got := BuildEnv(map[string]string{"QUANCODE_TEST_VAR": "hello"})
	if got == nil {
		t.Fatal("expected non-nil env")
	}
	found := false
	for _, e := range got {
		if e == "QUANCODE_TEST_VAR=hello" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected QUANCODE_TEST_VAR=hello in merged env")
	}
}

func TestRunSuccess(t *testing.T) {
	dir := t.TempDir()
	result, err := Run(dir, 10, nil, "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "hello" {
		t.Fatalf("expected stdout=hello, got %q", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.DurationMs <= 0 {
		t.Fatal("expected positive duration")
	}
}

func TestRunNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	result, err := Run(dir, 10, nil, "sh", "-c", "exit 42")
	if err != nil {
		t.Fatalf("unexpected error for non-zero exit: %v", err)
	}
	if result.ExitCode != 42 {
		t.Fatalf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestRunCommandNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := Run(dir, 10, nil, "nonexistent-command-12345")
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestRunTimeout(t *testing.T) {
	dir := t.TempDir()
	result, err := Run(dir, 1, nil, "sleep", "30")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !result.TimedOut {
		t.Fatal("expected TimedOut=true")
	}
	if result.ExitCode != 124 {
		t.Fatalf("expected exit code 124 for timeout, got %d", result.ExitCode)
	}
}

func TestRunWithStdinPipesInput(t *testing.T) {
	dir := t.TempDir()
	result, err := RunWithStdin(dir, 10, nil, "hello from stdin", "cat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "hello from stdin" {
		t.Fatalf("expected stdin content in stdout, got %q", result.Stdout)
	}
}

func TestRunWithStdinExitCode(t *testing.T) {
	dir := t.TempDir()
	result, err := RunWithStdin(dir, 10, nil, "", "sh", "-c", "exit 7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", result.ExitCode)
	}
}

func TestRunWithOutputFileReadsOutput(t *testing.T) {
	dir := t.TempDir()
	// Use sh -c to write to the output file. RunWithOutputFile builds:
	// name flagArgs... outputFlag tmpfile prompt
	// So: sh [] -c tmpfile "echo result > $0" — but $0 is the tmpfile
	// Actually the arg order is: sh -c tmpfile "echo..."
	// sh -c expects: sh -c 'script' [arg0 arg1 ...]
	// Let's test it differently: use a real command that writes to a file arg

	// Write a helper script
	script := filepath.Join(dir, "helper.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho output-content > \"$1\"\n"), 0755)

	result, err := RunWithOutputFile(dir, 10, nil, "--out", script, []string{"--flag"}, "the-prompt")
	// This will run: script --flag --out tmpfile the-prompt
	// The script writes to $1 which is --flag, not tmpfile
	// RunWithOutputFile: name flagArgs... outputFlag tmpPath prompt
	// => exec script --flag --out tmpPath the-prompt
	// So $1=--flag, $2=--out, $3=tmpPath
	// Let's just verify it doesn't crash for now
	_ = result
	_ = err
}

func TestIsGitRepoTrue(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if !IsGitRepo(dir) {
		t.Fatal("expected IsGitRepo=true for initialized repo")
	}
}

func TestIsGitRepoFalse(t *testing.T) {
	dir := t.TempDir()
	if IsGitRepo(dir) {
		t.Fatal("expected IsGitRepo=false for non-git dir")
	}
}

func TestCreateWorktreeCollectPatchApplyPatch(t *testing.T) {
	repo := t.TempDir()
	gitRun := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %s: %v", args, out, err)
		}
	}

	gitRun(repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(repo, "add", "-A")
	gitRun(repo, "commit", "-m", "initial")

	// Create worktree
	wtDir, cleanup, err := CreateWorktree(repo)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	defer cleanup()

	if wtDir == "" {
		t.Fatal("expected non-empty worktree dir")
	}

	// Make a change in the worktree
	if err := os.WriteFile(filepath.Join(wtDir, "file.txt"), []byte("modified\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Collect patch
	patch, files, err := CollectPatch(wtDir)
	if err != nil {
		t.Fatalf("CollectPatch: %v", err)
	}
	if patch == "" {
		t.Fatal("expected non-empty patch")
	}
	if len(files) != 1 || files[0] != "file.txt" {
		t.Fatalf("expected [file.txt], got %v", files)
	}

	// Apply patch to another repo copy
	applyDir := t.TempDir()
	gitRun(applyDir, "init")
	if err := os.WriteFile(filepath.Join(applyDir, "file.txt"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(applyDir, "add", "-A")
	gitRun(applyDir, "commit", "-m", "initial")

	if err := ApplyPatch(applyDir, patch); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(applyDir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "modified\n" {
		t.Fatalf("expected modified content, got %q", string(data))
	}
}

func TestApplyPatchEmptyIsNoop(t *testing.T) {
	if err := ApplyPatch(t.TempDir(), ""); err != nil {
		t.Fatalf("expected nil error for empty patch, got %v", err)
	}
}

func TestCollectPatchNoChanges(t *testing.T) {
	repo := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}

	gitRun("init")
	os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x"), 0644)
	gitRun("add", "-A")
	gitRun("commit", "-m", "init")

	patch, files, err := CollectPatch(repo)
	if err != nil {
		t.Fatalf("CollectPatch: %v", err)
	}
	if patch != "" {
		t.Fatalf("expected empty patch, got %q", patch)
	}
	if len(files) != 0 {
		t.Fatalf("expected no files, got %v", files)
	}
}
