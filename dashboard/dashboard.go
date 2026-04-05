// Package dashboard provides health-checking and background launching of the
// QuanCode web dashboard.
package dashboard

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Probe checks whether a QuanCode dashboard is already listening on the given port.
// Returns true if a healthy dashboard instance responds, false otherwise.
func Probe(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// Quick TCP check first to fail fast.
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()

	// HTTP health check — verify it's actually QuanCode, not some other service.
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/api/version", addr))
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	_, ok := body["version"]
	return ok
}

// LaunchBackground starts a detached dashboard process on the given port.
// The process runs independently of the parent and survives parent exit.
// Returns nil on successful spawn (does not wait for the server to be ready).
func LaunchBackground(port int) error {
	quancodeBin, err := os.Executable()
	if err != nil {
		if p, e := exec.LookPath("quancode"); e == nil {
			quancodeBin = p
		} else {
			return fmt.Errorf("cannot locate quancode binary: %w", err)
		}
	}

	args := []string{quancodeBin, "dashboard", "--port", fmt.Sprintf("%d", port)}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return fmt.Errorf("open /dev/null: %w", err)
	}

	logPath := dashboardLogPath()
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		devNull.Close()
		return fmt.Errorf("open dashboard log %s: %w", logPath, err)
	}

	attr := &os.ProcAttr{
		Dir:   "/",
		Files: []*os.File{devNull, logFile, logFile},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	}

	proc, err := os.StartProcess(quancodeBin, args, attr)
	if err != nil {
		devNull.Close()
		logFile.Close()
		return fmt.Errorf("start dashboard process: %w", err)
	}
	proc.Release()
	devNull.Close()
	logFile.Close()
	return nil
}

// EnsureRunning starts the dashboard if no instance is already running on the port.
// Returns (url, started, error).
// - url is non-empty when a dashboard is available (either existing or newly started).
// - started is true if a new instance was launched.
func EnsureRunning(port int) (url string, started bool, err error) {
	addr := fmt.Sprintf("http://127.0.0.1:%d", port)

	if Probe(port) {
		return addr, false, nil
	}

	// Check if port is occupied by something else.
	conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if dialErr == nil {
		conn.Close()
		return "", false, fmt.Errorf("port %d is in use by another service", port)
	}

	if err := LaunchBackground(port); err != nil {
		return "", false, err
	}
	return addr, true, nil
}

func dashboardLogPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		dir := fmt.Sprintf("%s/.config/quancode", home)
		_ = os.MkdirAll(dir, 0755)
		return dir + "/dashboard.log"
	}
	return "dashboard.log"
}
