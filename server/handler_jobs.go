package server

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/qq418716640/quancode/job"
)

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 50
	if l := q.Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}

	workDir := q.Get("workdir")
	jobs, err := job.ListJobs(workDir, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list jobs: "+err.Error())
		return
	}

	// Filter by status
	if status := q.Get("status"); status != "" {
		var filtered []*job.State
		for _, j := range jobs {
			if j.Status == status {
				filtered = append(filtered, j)
			}
		}
		jobs = filtered
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"jobs": jobs,
	})
}

func (s *Server) handleJobDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing job id")
		return
	}

	state, err := job.ReadState(id)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "job not found: "+id)
			return
		}
		writeError(w, http.StatusInternalServerError, "read job: "+err.Error())
		return
	}

	state = job.DetectLost(state)
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleJobOutput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing job id")
		return
	}

	path := job.OutputPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "output not found for job: "+id)
			return
		}
		writeError(w, http.StatusInternalServerError, "read output: "+err.Error())
		return
	}

	// Tail support: return last N lines (max 10000)
	tail := 500
	if t := r.URL.Query().Get("tail"); t != "" {
		if v, err := strconv.Atoi(t); err == nil && v > 0 && v <= 10000 {
			tail = v
		}
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(strings.Join(lines, "\n")))
}
