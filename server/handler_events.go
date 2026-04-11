package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/qq418716640/quancode/job"
	"github.com/qq418716640/quancode/ledger"
)

const (
	maxSSEClients = 10
	pollInterval  = 3 * time.Second
)

// sseHub manages a single polling goroutine that broadcasts to all SSE clients.
type sseHub struct {
	mu       sync.Mutex
	clients  map[chan []byte]struct{}
	started  bool
	demoMode bool
}

func newSSEHub(demoMode bool) *sseHub {
	return &sseHub{clients: make(map[chan []byte]struct{}), demoMode: demoMode}
}

func (h *sseHub) addClient(ch chan []byte) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) >= maxSSEClients {
		return false
	}
	h.clients[ch] = struct{}{}
	if !h.started && !h.demoMode {
		h.started = true
		go h.poll()
	}
	return true
}

func (h *sseHub) removeClient(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, ch)
	close(ch)
}

func (h *sseHub) broadcast(data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- data:
		default:
			// Drop message for slow clients
		}
	}
}

func (h *sseHub) poll() {
	lastEntryCount := -1
	lastJobStates := map[string]string{} // jobID -> status

	for {
		time.Sleep(pollInterval)

		h.mu.Lock()
		active := len(h.clients)
		if active == 0 {
			h.started = false
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()

		// Check for new delegations
		entries, err := ledger.ReadAll()
		if err != nil {
			log.Printf("SSE poll: ledger.ReadAll: %v", err)
		} else {
			if lastEntryCount >= 0 && len(entries) > lastEntryCount {
				for _, e := range entries[lastEntryCount:] {
					if msg, err := json.Marshal(map[string]any{"type": "delegation", "data": e}); err == nil {
						h.broadcast(msg)
					}
				}
			}
			lastEntryCount = len(entries)
		}

		// Check for job status changes; only track non-terminal jobs
		jobs, err := job.ListJobs("", 0)
		if err != nil {
			log.Printf("SSE poll: job.ListJobs: %v", err)
		} else {
			currentIDs := make(map[string]bool, len(jobs))
			for _, j := range jobs {
				currentIDs[j.JobID] = true
				prev, exists := lastJobStates[j.JobID]
				if !exists || prev != j.Status {
					if msg, err := json.Marshal(map[string]any{"type": "job_update", "data": j}); err == nil {
						h.broadcast(msg)
					}
					lastJobStates[j.JobID] = j.Status
				}
			}
			// Clean up entries for jobs that no longer exist
			for id := range lastJobStates {
				if !currentIDs[id] {
					delete(lastJobStates, id)
				}
			}
		}
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ch := make(chan []byte, 16)
	if !s.hub.addClient(ch) {
		writeError(w, http.StatusTooManyRequests, "too many SSE connections")
		return
	}
	defer s.hub.removeClient(ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ctx := r.Context()
	fmt.Fprintf(w, "data: {\"type\":\"connected\"}\n\n")
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}
