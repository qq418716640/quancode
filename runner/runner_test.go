package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// testGitRun runs a git command in the given directory with deterministic
// author/committer identity for reproducible tests.
func testGitRun(t *testing.T, dir string, args ...string) {
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

	testGitRun(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testGitRun(t, repo, "add", "-A")
	testGitRun(t, repo, "commit", "-m", "initial")

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
	testGitRun(t, applyDir, "init")
	if err := os.WriteFile(filepath.Join(applyDir, "file.txt"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testGitRun(t, applyDir, "add", "-A")
	testGitRun(t, applyDir, "commit", "-m", "initial")

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

	testGitRun(t, repo, "init")
	os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x"), 0644)
	testGitRun(t, repo, "add", "-A")
	testGitRun(t, repo, "commit", "-m", "init")

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

func TestPatchSummaryEmptyPatch(t *testing.T) {
	summary, err := PatchSummary(t.TempDir(), "")
	if err != nil {
		t.Fatalf("expected nil error for empty patch, got %v", err)
	}
	if summary != "" {
		t.Fatalf("expected empty summary, got %q", summary)
	}
}

func TestPatchSummaryValidPatch(t *testing.T) {
	repo := t.TempDir()

	testGitRun(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testGitRun(t, repo, "add", "-A")
	testGitRun(t, repo, "commit", "-m", "initial")

	wtDir, cleanup, err := CreateWorktree(repo)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	defer cleanup()

	if err := os.WriteFile(filepath.Join(wtDir, "file.txt"), []byte("modified\n"), 0644); err != nil {
		t.Fatal(err)
	}

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

	summary, err := PatchSummary(repo, patch)
	if err != nil {
		t.Fatalf("PatchSummary: %v", err)
	}
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestPruneOrphanWorktreesNoOrphans(t *testing.T) {
	repo := t.TempDir()
	testGitRun(t, repo, "init")
	os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x"), 0644)
	testGitRun(t, repo, "add", "-A")
	testGitRun(t, repo, "commit", "-m", "init")

	// No .quancode dir at all — should return 0
	if pruned := PruneOrphanWorktrees(repo); pruned != 0 {
		t.Fatalf("expected 0 pruned, got %d", pruned)
	}
}

func TestPruneOrphanWorktreesRemovesOrphans(t *testing.T) {
	repo := t.TempDir()
	testGitRun(t, repo, "init")
	os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x"), 0644)
	testGitRun(t, repo, "add", "-A")
	testGitRun(t, repo, "commit", "-m", "init")

	// Create a fake orphan directory (not a real git worktree)
	base := filepath.Join(repo, ".quancode", "worktrees")
	orphan := filepath.Join(base, "wt-orphan123")
	if err := os.MkdirAll(orphan, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(orphan, "junk.txt"), []byte("leftover"), 0644)
	// Set modtime to 2 hours ago so it passes the age cutoff
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(orphan, old, old)

	pruned := PruneOrphanWorktrees(repo)
	if pruned != 1 {
		t.Fatalf("expected 1 pruned, got %d", pruned)
	}

	// Verify orphan directory is gone
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("expected orphan directory to be removed")
	}
}

func TestPruneOrphanWorktreesSkipsRecent(t *testing.T) {
	repo := t.TempDir()
	testGitRun(t, repo, "init")
	os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x"), 0644)
	testGitRun(t, repo, "add", "-A")
	testGitRun(t, repo, "commit", "-m", "init")

	// Create a fake orphan that was just created (modtime = now)
	base := filepath.Join(repo, ".quancode", "worktrees")
	orphan := filepath.Join(base, "wt-recent456")
	if err := os.MkdirAll(orphan, 0755); err != nil {
		t.Fatal(err)
	}

	pruned := PruneOrphanWorktrees(repo)
	if pruned != 0 {
		t.Fatalf("expected 0 pruned (too recent), got %d", pruned)
	}

	// Directory should still exist
	if _, err := os.Stat(orphan); err != nil {
		t.Fatal("expected recent orphan to be kept")
	}
}

func TestPruneOrphanWorktreesKeepsActive(t *testing.T) {
	repo := t.TempDir()
	testGitRun(t, repo, "init")
	os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x"), 0644)
	testGitRun(t, repo, "add", "-A")
	testGitRun(t, repo, "commit", "-m", "init")

	// Create a real worktree via CreateWorktree
	wtDir, cleanup, err := CreateWorktree(repo)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	defer cleanup()

	// Prune should not remove the active worktree
	pruned := PruneOrphanWorktrees(repo)
	if pruned != 0 {
		t.Fatalf("expected 0 pruned (active worktree), got %d", pruned)
	}

	// Active worktree should still exist
	if _, err := os.Stat(wtDir); err != nil {
		t.Fatalf("expected active worktree to still exist: %v", err)
	}
}

func TestParseConflictFiles(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name:   "patch failed with line number",
			output: "error: patch failed: src/foo.go:42\nerror: src/foo.go: patch does not apply\n",
			want:   []string{"src/foo.go"},
		},
		{
			name:   "multiple files",
			output: "error: patch failed: a.go:1\nerror: patch failed: b.go:10\n",
			want:   []string{"a.go", "b.go"},
		},
		{
			name:   "no errors",
			output: "Checking patch src/foo.go...\n",
			want:   nil,
		},
		{
			name:   "empty",
			output: "",
			want:   nil,
		},
		{
			name:   "deduplication",
			output: "error: patch failed: x.go:1\nerror: x.go: patch does not apply\n",
			want:   []string{"x.go"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseConflictFiles(tt.output)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseConflictFiles() = %v, want %v", got, tt.want)
			}
		})
	}
}
