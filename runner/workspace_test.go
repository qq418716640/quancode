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

// TestEnsureWorktreeIgnoreSkipsBuildArtifacts is a regression test for the
// bug where ensureWorktreeIgnore wrote to the worktree-specific gitdir's
// info/exclude (which git ignores), letting .gocache/* and node_modules/*
// pollute collected patches. The fix writes to the common gitdir instead.
func TestEnsureWorktreeIgnoreSkipsBuildArtifacts(t *testing.T) {
	repo := initTestRepo(t)
	wt, cleanup, err := CreateWorktree(repo)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	defer cleanup()

	// Drop a file into each ignore pattern.
	for _, dir := range []string{".gocache/00", "node_modules/leaf", ".cache/x"} {
		full := filepath.Join(wt, dir)
		if err := os.MkdirAll(full, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(filepath.Join(full, "junk"), []byte("x"), 0644); err != nil {
			t.Fatalf("write %s/junk: %v", full, err)
		}
	}
	// Plus one real file we DO want to see in the patch.
	if err := os.WriteFile(filepath.Join(wt, "real.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, files, err := CollectPatch(wt)
	if err != nil {
		t.Fatalf("CollectPatch: %v", err)
	}

	for _, f := range files {
		if strings.HasPrefix(f, ".gocache/") || strings.HasPrefix(f, "node_modules/") || strings.HasPrefix(f, ".cache/") {
			t.Errorf("build artifact leaked into patch: %s", f)
		}
	}
	var sawReal bool
	for _, f := range files {
		if f == "real.go" {
			sawReal = true
		}
	}
	if !sawReal {
		t.Errorf("expected real.go in patch, got %v", files)
	}
}

// TestEnsureWorktreeIgnorePreservesUserRules — appending the quancode
// block must not corrupt or drop user-supplied rules already present in
// .git/info/exclude.
func TestEnsureWorktreeIgnorePreservesUserRules(t *testing.T) {
	repo := initTestRepo(t)
	excludePath := filepath.Join(repo, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0755); err != nil {
		t.Fatal(err)
	}
	userRules := "# user rules\nmy-private-notes/\n*.user.json\n"
	if err := os.WriteFile(excludePath, []byte(userRules), 0644); err != nil {
		t.Fatal(err)
	}

	wt, cleanup, err := CreateWorktree(repo)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	defer cleanup()

	got, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "my-private-notes/") || !strings.Contains(string(got), "*.user.json") {
		t.Errorf("user rules dropped, got:\n%s", got)
	}
	if !strings.Contains(string(got), quancodeExcludeMarker) {
		t.Errorf("quancode marker missing, got:\n%s", got)
	}

	// Verify the user rule actually filters in the worktree, alongside the
	// quancode rules — both blocks should be active.
	if err := os.WriteFile(filepath.Join(wt, "x.user.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, ".gocache"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".gocache", "x"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	_, files, err := CollectPatch(wt)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, ".user.json") || strings.HasPrefix(f, ".gocache/") {
			t.Errorf("rule block ineffective: %s slipped through (files=%v)", f, files)
		}
	}
}

// TestEnsureWorktreeIgnoreIsIdempotent — repeated worktree creation must
// not duplicate the rule block in the common info/exclude file.
func TestEnsureWorktreeIgnoreIsIdempotent(t *testing.T) {
	repo := initTestRepo(t)
	for i := 0; i < 3; i++ {
		_, cleanup, err := CreateWorktree(repo)
		if err != nil {
			t.Fatalf("CreateWorktree #%d: %v", i, err)
		}
		cleanup()
	}
	excludeData, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	count := strings.Count(string(excludeData), quancodeExcludeMarker)
	if count != 1 {
		t.Errorf("expected marker exactly once, got %d in:\n%s", count, excludeData)
	}
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
