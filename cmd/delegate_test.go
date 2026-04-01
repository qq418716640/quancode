package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
		output:       "done",
		patch:        "diff --git a/file b/file",
		changedFiles: []string{"file.go"},
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

func TestWarnContextSize(t *testing.T) {
	tests := []struct {
		name       string
		totalBytes int
		wantWarn   bool
	}{
		{name: "below threshold", totalBytes: 24*1024 - 1, wantWarn: false},
		{name: "at threshold", totalBytes: 24 * 1024, wantWarn: true},
		{name: "above threshold", totalBytes: 25 * 1024, wantWarn: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStderr(t, func() {
				warnContextSize(nil, tt.totalBytes)
			})

			if tt.wantWarn {
				if !strings.Contains(out, "[quancode] warning: total prompt size") {
					t.Fatalf("expected warning output, got %q", out)
				}
				return
			}

			if out != "" {
				t.Fatalf("expected no output, got %q", out)
			}
		})
	}
}

func TestWarnContextSizeNilBundleDoesNotPanic(t *testing.T) {
	captureStderr(t, func() {
		warnContextSize(nil, 0)
	})
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&buf, r)
		_ = r.Close()
		done <- copyErr
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	return buf.String()
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

func TestClassifyFailure(t *testing.T) {
	tests := []struct {
		name string
		ar   attemptResult
		want string
	}{
		{
			name: "success",
			ar:   attemptResult{result: &runner.Result{ExitCode: 0}},
			want: "",
		},
		{
			name: "launch failure",
			ar:   attemptResult{result: nil, err: errors.New("not found")},
			want: FailureClassLaunchFailure,
		},
		{
			name: "timed out",
			ar:   attemptResult{result: &runner.Result{TimedOut: true, ExitCode: 124}},
			want: FailureClassTimedOut,
		},
		{
			name: "agent failed non-zero exit",
			ar:   attemptResult{result: &runner.Result{ExitCode: 1}},
			want: FailureClassAgentFailed,
		},
		{
			name: "rate limited",
			ar: attemptResult{
				result: &runner.Result{ExitCode: 1},
				output: "Error: rate limit exceeded",
			},
			want: FailureClassRateLimited,
		},
		{
			name: "patch conflict",
			ar: attemptResult{
				result:        &runner.Result{ExitCode: 0},
				patchApplyErr: errors.New("conflict"),
			},
			want: FailureClassPatchConflict,
		},
		{
			name: "verify strict failure",
			ar: attemptResult{
				result: &runner.Result{ExitCode: 0},
				verify: &VerifyResult{Enabled: true, Strict: true, Status: VerifyFailed, FailedCount: 1},
			},
			want: FailureClassVerifyFailed,
		},
		{
			name: "verify non-strict is not failure",
			ar: attemptResult{
				result: &runner.Result{ExitCode: 0},
				verify: &VerifyResult{Enabled: true, Strict: false, Status: VerifyFailed, FailedCount: 1},
			},
			want: "",
		},
		{
			name: "nil result no error is empty",
			ar:   attemptResult{result: nil, err: nil},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyFailure(tt.ar)
			if got != tt.want {
				t.Errorf("classifyFailure() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsTransientFailure(t *testing.T) {
	transient := []string{FailureClassLaunchFailure, FailureClassTimedOut, FailureClassRateLimited}
	for _, fc := range transient {
		if !isTransientFailure(fc) {
			t.Errorf("%q should be transient", fc)
		}
	}
	nonTransient := []string{FailureClassAgentFailed, FailureClassPatchConflict, FailureClassVerifyFailed, FailureClassSpeculativeCancelled, ""}
	for _, fc := range nonTransient {
		if isTransientFailure(fc) {
			t.Errorf("%q should not be transient", fc)
		}
	}
}
