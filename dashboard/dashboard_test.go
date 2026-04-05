package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestProbe_NoServer(t *testing.T) {
	// Port with nothing listening should return false quickly.
	if Probe(19999) {
		t.Error("Probe should return false when nothing is listening")
	}
}

func TestProbe_HealthyServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"version": "v0.1.0"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	port := mustPort(t, srv.URL)
	if !Probe(port) {
		t.Error("Probe should return true for a healthy QuanCode dashboard")
	}
}

func TestProbe_NonQuanCodeServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	port := mustPort(t, srv.URL)
	if Probe(port) {
		t.Error("Probe should return false for a non-QuanCode server")
	}
}

func TestEnsureRunning_AlreadyRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"version": "v0.1.0"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	port := mustPort(t, srv.URL)
	url, started, err := EnsureRunning(port)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if started {
		t.Error("should not report started when already running")
	}
	if url == "" {
		t.Error("url should be non-empty")
	}
}

func TestEnsureRunning_PortConflict(t *testing.T) {
	// Start a non-QuanCode server to occupy a port.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not quancode"))
	}))
	defer srv.Close()

	port := mustPort(t, srv.URL)
	_, _, err := EnsureRunning(port)
	if err == nil {
		t.Error("expected error when port is occupied by non-QuanCode service")
	}
}

func mustPort(t *testing.T, rawURL string) int {
	t.Helper()
	// httptest URLs are like "http://127.0.0.1:PORT"
	for i := len(rawURL) - 1; i >= 0; i-- {
		if rawURL[i] == ':' {
			p, err := strconv.Atoi(rawURL[i+1:])
			if err != nil {
				t.Fatalf("parse port from %q: %v", rawURL, err)
			}
			return p
		}
	}
	t.Fatalf("no port in URL %q", rawURL)
	return 0
}
