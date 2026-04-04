package server

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/qq418716640/quancode/active"
	"github.com/qq418716640/quancode/job"
	"github.com/qq418716640/quancode/ledger"
)

type statsCache struct {
	mu        sync.Mutex
	data      map[string]any
	updatedAt time.Time
	ttl       time.Duration
}

func newStatsCache(ttl time.Duration) *statsCache {
	return &statsCache{ttl: ttl}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	// Active tasks are always computed fresh (not cached).
	activeSyncTasks := len(active.List())
	activeAsyncJobs := 0
	jobs, _ := job.ListJobs("", 0)
	for _, j := range jobs {
		if !job.IsTerminal(j.Status) {
			activeAsyncJobs++
		}
	}
	activeTasks := activeSyncTasks + activeAsyncJobs

	// Ledger stats use cache.
	s.stats.mu.Lock()
	if s.stats.data != nil && time.Since(s.stats.updatedAt) < s.stats.ttl {
		data := copyMap(s.stats.data)
		s.stats.mu.Unlock()
		data["active_tasks"] = activeTasks
		writeJSON(w, http.StatusOK, data)
		return
	}
	s.stats.mu.Unlock()

	entries, err := ledger.ReadAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read ledger: "+err.Error())
		return
	}

	total := len(entries)
	succeeded := 0
	var totalDuration int64
	agentCounts := map[string]int{}
	todayCount := 0
	today := time.Now().Format("2006-01-02")

	for _, e := range entries {
		if e.ExitCode == 0 {
			succeeded++
		}
		totalDuration += e.DurationMs
		agentCounts[e.Agent]++
		if len(e.Timestamp) >= 10 && e.Timestamp[:10] == today {
			todayCount++
		}
	}

	var successRate string
	var avgDuration string
	if total > 0 {
		successRate = fmt.Sprintf("%.0f%%", float64(succeeded)/float64(total)*100)
		avgMs := totalDuration / int64(total)
		if avgMs < 1000 {
			avgDuration = fmt.Sprintf("%dms", avgMs)
		} else if avgMs < 60000 {
			avgDuration = fmt.Sprintf("%.1fs", float64(avgMs)/1000)
		} else {
			avgDuration = fmt.Sprintf("%.1fm", float64(avgMs)/60000)
		}
	}

	result := map[string]any{
		"total":        total,
		"succeeded":    succeeded,
		"success_rate": successRate,
		"avg_duration": avgDuration,
		"agents":       agentCounts,
		"today":        todayCount,
	}

	s.stats.mu.Lock()
	s.stats.data = result
	s.stats.updatedAt = time.Now()
	s.stats.mu.Unlock()

	out := copyMap(result)
	out["active_tasks"] = activeTasks
	writeJSON(w, http.StatusOK, out)
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
