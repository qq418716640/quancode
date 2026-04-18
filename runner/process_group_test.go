package runner

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunKillsProcessGroup(t *testing.T) {
	// Spawn a shell that forks a child (sleep 300), then verify both are killed on timeout.
	// The shell script: start a background sleep, print its PID, then sleep forever.
	dir := t.TempDir()
	result, err := Run(dir, 2, nil, "sh", "-c",
		"sleep 300 & echo child=$!; sleep 300")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !result.TimedOut {
		t.Fatal("expected TimedOut=true")
	}

	// Extract child PID from stdout
	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.HasPrefix(line, "child=") {
			pidStr := strings.TrimPrefix(line, "child=")
			// Give a moment for cleanup
			time.Sleep(100 * time.Millisecond)
			// Check if child process is still alive
			check := exec.Command("kill", "-0", pidStr)
			if check.Run() == nil {
				t.Errorf("child process %s still alive after process group kill", pidStr)
				// Clean up
				exec.Command("kill", "-9", pidStr).Run()
			}
		}
	}
}

func TestRunWithContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()

	done := make(chan struct{})
	var result *Result
	var err error
	go func() {
		result, err = RunWithContext(ctx, dir, nil, "sleep", "300")
		close(done)
	}()

	// Cancel after 500ms
	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Good — command was cancelled
	case <-time.After(10 * time.Second):
		t.Fatal("RunWithContext did not return after cancel")
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Cancelled {
		t.Fatal("expected Cancelled=true after context cancel (caller-initiated)")
	}
	if result.TimedOut {
		t.Fatal("expected TimedOut=false — this cancellation was not a deadline")
	}
	_ = err
}

func TestRunWithContextSuccess(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	result, err := RunWithContext(ctx, dir, nil, "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "hello" {
		t.Fatalf("expected stdout=hello, got %q", result.Stdout)
	}
	if result.TimedOut {
		t.Fatal("expected TimedOut=false")
	}
}

func TestRunWithStdinContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()

	done := make(chan struct{})
	go func() {
		RunWithStdinContext(ctx, dir, nil, "ignored", "sleep", "300")
		close(done)
	}()

	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RunWithStdinContext did not return after cancel")
	}
}
