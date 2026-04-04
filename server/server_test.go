package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNew(t *testing.T) {
	s := New("127.0.0.1:0", false)
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.addr != "127.0.0.1:0" {
		t.Errorf("addr = %q, want %q", s.addr, "127.0.0.1:0")
	}
}

func TestHandleDelegations(t *testing.T) {
	s := New("127.0.0.1:0", false)
	req := httptest.NewRequest("GET", "/api/delegations?limit=5", nil)
	w := httptest.NewRecorder()
	s.handleDelegations(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := resp["total"]; !ok {
		t.Error("response missing 'total' field")
	}
	if _, ok := resp["entries"]; !ok {
		t.Error("response missing 'entries' field")
	}
}

func TestHandleDelegationsInvalidSince(t *testing.T) {
	s := New("127.0.0.1:0", false)
	req := httptest.NewRequest("GET", "/api/delegations?since=bad", nil)
	w := httptest.NewRecorder()
	s.handleDelegations(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleJobs(t *testing.T) {
	s := New("127.0.0.1:0", false)
	req := httptest.NewRequest("GET", "/api/jobs?limit=5", nil)
	w := httptest.NewRecorder()
	s.handleJobs(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestHandleJobDetailNotFound(t *testing.T) {
	s := New("127.0.0.1:0", false)
	req := httptest.NewRequest("GET", "/api/jobs/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()
	s.handleJobDetail(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleJobOutputNotFound(t *testing.T) {
	s := New("127.0.0.1:0", false)
	req := httptest.NewRequest("GET", "/api/jobs/nonexistent/output", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()
	s.handleJobOutput(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleStats(t *testing.T) {
	s := New("127.0.0.1:0", false)
	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	s.handleStats(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"total", "succeeded", "success_rate", "avg_duration", "active_jobs"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("response missing %q field", key)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]string{"key": "value"})

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "test error")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["error"] != "test error" {
		t.Errorf("error = %q, want %q", resp["error"], "test error")
	}
}

func TestMiddlewareSecurityHeaders(t *testing.T) {
	s := New("127.0.0.1:0", false)
	handler := s.withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if v := w.Header().Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", v, "nosniff")
	}
}

func TestMiddlewareCORSDevMode(t *testing.T) {
	s := New("127.0.0.1:0", true) // dev mode
	handler := s.withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "*" {
		t.Errorf("CORS origin = %q, want %q", v, "*")
	}
}
