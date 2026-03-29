package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/qq418716640/quancode/runner"
)

func TestDetectNewChangesReturnsOnlyNewFiles(t *testing.T) {
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.name", "QuanCode Test")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")

	writeTestFile(t, filepath.Join(dir, "tracked.txt"), "base\n")
	runGitCmd(t, dir, "add", "tracked.txt")
	runGitCmd(t, dir, "commit", "-m", "init")

	pre := gitStatusSnapshot(dir)

	writeTestFile(t, filepath.Join(dir, "tracked.txt"), "base\nchanged\n")
	writeTestFile(t, filepath.Join(dir, "new.txt"), "new\n")

	got := detectNewChanges(dir, pre)
	sort.Strings(got)
	want := []string{"new.txt", "tracked.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("detectNewChanges mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestDetectNewChangesIgnoresPreexistingDirtyFiles(t *testing.T) {
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.name", "QuanCode Test")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")

	writeTestFile(t, filepath.Join(dir, "tracked.txt"), "base\n")
	runGitCmd(t, dir, "add", "tracked.txt")
	runGitCmd(t, dir, "commit", "-m", "init")

	writeTestFile(t, filepath.Join(dir, "tracked.txt"), "already dirty\n")
	pre := gitStatusSnapshot(dir)

	writeTestFile(t, filepath.Join(dir, "new.txt"), "new\n")

	got := detectNewChanges(dir, pre)
	sort.Strings(got)
	want := []string{"new.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("detectNewChanges mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestDetectNewChangesReportsDeletedFiles(t *testing.T) {
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.name", "QuanCode Test")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")

	writeTestFile(t, filepath.Join(dir, "tracked.txt"), "base\n")
	runGitCmd(t, dir, "add", "tracked.txt")
	runGitCmd(t, dir, "commit", "-m", "init")

	pre := gitStatusSnapshot(dir)

	if err := os.Remove(filepath.Join(dir, "tracked.txt")); err != nil {
		t.Fatalf("remove tracked file: %v", err)
	}

	got := detectNewChanges(dir, pre)
	want := []string{"tracked.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("detectNewChanges mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestDetectNewChangesReturnsNilOutsideGitRepo(t *testing.T) {
	got := detectNewChanges(t.TempDir(), nil)
	if got != nil {
		t.Fatalf("expected nil outside git repo, got %#v", got)
	}
}

func TestBuildDelegationResultJSONFields(t *testing.T) {
	ar := attemptResult{
		result: &runner.Result{
			ExitCode:   0,
			TimedOut:   false,
			DurationMs: 42,
		},
		output:         "done",
		patch:          "diff --git a/file b/file",
		changedFiles:   []string{"file.go"},
	}

	got := buildDelegationResult("codex", "write tests", "patch", ar)

	if got.Agent != "codex" || got.Task != "write tests" {
		t.Fatalf("unexpected identity fields: %#v", got)
	}
	if got.Output != "done" || got.Patch == "" {
		t.Fatalf("expected output and patch to be populated: %#v", got)
	}
	if len(got.ChangedFiles) != 1 || got.ChangedFiles[0] != "file.go" {
		t.Fatalf("unexpected changed files: %#v", got.ChangedFiles)
	}
	if got.Status != "completed" {
		t.Fatalf("expected completed status, got %q", got.Status)
	}

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var decoded DelegationResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if decoded.Agent != "codex" || decoded.Isolation != "patch" {
		t.Fatalf("unexpected decoded payload: %#v", decoded)
	}
}

func TestBuildDelegationResultErrorForcesExitCodeOneWhenUnset(t *testing.T) {
	ar := attemptResult{
		output: "partial output",
		err:    errors.New("boom"),
	}

	got := buildDelegationResult("codex", "write tests", "inplace", ar)

	if got.ExitCode != 1 {
		t.Fatalf("expected exit code 1 on error without result, got %d", got.ExitCode)
	}
	if got.Output != "partial output" {
		t.Fatalf("expected output to be preserved on error, got %q", got.Output)
	}
	if got.Status != "failed" {
		t.Fatalf("expected failed status, got %q", got.Status)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
