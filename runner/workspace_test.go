package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo creates a git repo with one committed file.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %s: %v", strings.Join(args, " "), out, err)
		}
	}
	run("init")
	run("checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-m", "initial")
	return dir
}

func TestApplyDiffToWorktreeWorking(t *testing.T) {
	repo := initTestRepo(t)

	// Make an uncommitted (working) change.
	if err := os.WriteFile(filepath.Join(repo, "hello.txt"), []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create worktree — it should NOT have the working change.
	wt, cleanup, err := CreateWorktree(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	content, _ := os.ReadFile(filepath.Join(wt, "hello.txt"))
	if string(content) != "hello\n" {
		t.Fatalf("worktree should have original content before apply, got: %q", content)
	}

	// Apply working diff.
	baseline, err := ApplyDiffToWorktree(repo, wt, "working")
	if err != nil {
		t.Fatalf("ApplyDiffToWorktree: %v", err)
	}
	if baseline == "" {
		t.Fatal("expected non-empty baseline SHA")
	}

	// Worktree should now have the change.
	content, _ = os.ReadFile(filepath.Join(wt, "hello.txt"))
	if string(content) != "hello world\n" {
		t.Fatalf("worktree should have updated content after apply, got: %q", content)
	}

	// CollectPatchSince(baseline) should be empty — no agent changes yet.
	patch, files, err := CollectPatchSince(wt, baseline)
	if err != nil {
		t.Fatalf("CollectPatchSince: %v", err)
	}
	if patch != "" || len(files) > 0 {
		t.Fatalf("expected empty patch (no agent changes), got patch=%q files=%v", patch, files)
	}

	// Simulate agent change.
	if err := os.WriteFile(filepath.Join(wt, "new.txt"), []byte("agent output\n"), 0644); err != nil {
		t.Fatal(err)
	}

	patch, files, err = CollectPatchSince(wt, baseline)
	if err != nil {
		t.Fatalf("CollectPatchSince after agent change: %v", err)
	}
	if !strings.Contains(patch, "new.txt") {
		t.Fatalf("patch should contain agent's new.txt, got: %q", patch)
	}
	if len(files) != 1 || files[0] != "new.txt" {
		t.Fatalf("expected files=[new.txt], got %v", files)
	}
	// Patch should NOT contain hello.txt (context diff).
	if strings.Contains(patch, "hello.txt") {
		t.Fatal("patch should not contain context-diff file hello.txt")
	}
}

func TestApplyDiffToWorktreeStaged(t *testing.T) {
	repo := initTestRepo(t)

	// Stage a change.
	if err := os.WriteFile(filepath.Join(repo, "hello.txt"), []byte("staged change\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "hello.txt")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %s: %v", out, err)
	}

	wt, cleanup, err := CreateWorktree(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	baseline, err := ApplyDiffToWorktree(repo, wt, "staged")
	if err != nil {
		t.Fatalf("ApplyDiffToWorktree staged: %v", err)
	}
	if baseline == "" {
		t.Fatal("expected non-empty baseline SHA for staged diff")
	}

	content, _ := os.ReadFile(filepath.Join(wt, "hello.txt"))
	if string(content) != "staged change\n" {
		t.Fatalf("worktree should have staged content, got: %q", content)
	}
}

func TestApplyDiffToWorktreeEmpty(t *testing.T) {
	repo := initTestRepo(t)

	// No uncommitted changes.
	wt, cleanup, err := CreateWorktree(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	baseline, err := ApplyDiffToWorktree(repo, wt, "working")
	if err != nil {
		t.Fatalf("ApplyDiffToWorktree empty: %v", err)
	}
	if baseline != "" {
		t.Fatalf("expected empty baseline for empty diff, got: %s", baseline)
	}
}
