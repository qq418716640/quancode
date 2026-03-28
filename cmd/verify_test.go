package cmd

import (
	"fmt"
	"testing"

	"github.com/qq418716640/quancode/runner"
)

func TestRunVerification_NoCmds(t *testing.T) {
	vr := runVerification("/tmp", nil, 120, false)
	if vr.Status != "skipped" {
		t.Errorf("expected skipped, got %s", vr.Status)
	}
}

func TestRunVerification_AllPass(t *testing.T) {
	orig := runVerifyCommand
	defer func() { runVerifyCommand = orig }()

	runVerifyCommand = func(workDir string, timeout int, env []string, name string, args ...string) (*runner.Result, error) {
		return &runner.Result{ExitCode: 0, DurationMs: 100}, nil
	}

	vr := runVerification("/tmp", []string{"go test ./...", "go vet ./..."}, 120, true)
	if vr.Status != "passed" {
		t.Errorf("expected passed, got %s", vr.Status)
	}
	if vr.PassedCount != 2 {
		t.Errorf("expected 2 passed, got %d", vr.PassedCount)
	}
	if vr.FailedCount != 0 {
		t.Errorf("expected 0 failed, got %d", vr.FailedCount)
	}
}

func TestRunVerification_PartialFailure(t *testing.T) {
	orig := runVerifyCommand
	defer func() { runVerifyCommand = orig }()

	callCount := 0
	runVerifyCommand = func(workDir string, timeout int, env []string, name string, args ...string) (*runner.Result, error) {
		callCount++
		if callCount == 1 {
			return &runner.Result{ExitCode: 0, DurationMs: 50}, nil
		}
		return &runner.Result{ExitCode: 1, DurationMs: 80, Stderr: "test failed"}, nil
	}

	vr := runVerification("/tmp", []string{"go test ./...", "go vet ./..."}, 120, true)
	if vr.Status != "failed" {
		t.Errorf("expected failed, got %s", vr.Status)
	}
	if vr.PassedCount != 1 {
		t.Errorf("expected 1 passed, got %d", vr.PassedCount)
	}
	if vr.FailedCount != 1 {
		t.Errorf("expected 1 failed, got %d", vr.FailedCount)
	}
}

func TestRunVerification_Timeout(t *testing.T) {
	orig := runVerifyCommand
	defer func() { runVerifyCommand = orig }()

	runVerifyCommand = func(workDir string, timeout int, env []string, name string, args ...string) (*runner.Result, error) {
		return &runner.Result{ExitCode: 124, TimedOut: true, DurationMs: 120000}, nil
	}

	vr := runVerification("/tmp", []string{"slow-test"}, 120, false)
	if vr.Status != "failed" {
		t.Errorf("expected failed, got %s", vr.Status)
	}
	if vr.Results[0].Status != "timed_out" {
		t.Errorf("expected timed_out, got %s", vr.Results[0].Status)
	}
}

func TestRunVerification_CommandNotFound(t *testing.T) {
	orig := runVerifyCommand
	defer func() { runVerifyCommand = orig }()

	runVerifyCommand = func(workDir string, timeout int, env []string, name string, args ...string) (*runner.Result, error) {
		return nil, fmt.Errorf("exec: \"nonexistent\": executable file not found in $PATH")
	}

	vr := runVerification("/tmp", []string{"nonexistent"}, 120, true)
	if vr.Status != "failed" {
		t.Errorf("expected failed, got %s", vr.Status)
	}
	if vr.Results[0].Status != "error" {
		t.Errorf("expected error, got %s", vr.Results[0].Status)
	}
	if vr.Results[0].Error == "" {
		t.Error("expected error message")
	}
}

func TestRunVerification_StrictFlag(t *testing.T) {
	orig := runVerifyCommand
	defer func() { runVerifyCommand = orig }()

	runVerifyCommand = func(workDir string, timeout int, env []string, name string, args ...string) (*runner.Result, error) {
		return &runner.Result{ExitCode: 0, DurationMs: 50}, nil
	}

	vr := runVerification("/tmp", []string{"test"}, 120, true)
	if !vr.Strict {
		t.Error("strict should be true")
	}
	if !vr.Enabled {
		t.Error("enabled should be true")
	}
}
