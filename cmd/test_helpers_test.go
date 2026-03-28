package cmd

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// These helpers mutate package-level command flags and process-wide stdout/cwd.
// Keep tests in this package serial; do not use t.Parallel().

// isolateHome sets HOME to a temp directory so ledger writes don't pollute
// the real ~/.config/quancode/logs/. Returns a cleanup function via t.Cleanup.
func isolateHome(t *testing.T) {
	t.Helper()
	oldHome := os.Getenv("HOME")
	home := t.TempDir()
	os.Setenv("HOME", home)
	t.Cleanup(func() { os.Setenv("HOME", oldHome) })
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

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
		t.Fatalf("read captured stdout: %v", err)
	}
	return buf.String()
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	fn()
}

func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "quancode.yaml")
	writeTestFile(t, path, content)
	return path
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
