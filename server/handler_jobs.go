package server

import (
	"net/http"
	"os"
	"strconv"
	"time"

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

	// Filter by agent
	if agent := q.Get("agent"); agent != "" {
		var filtered []*job.State
		for _, j := range jobs {
			actual := j.ActualAgent
			if actual == "" {
				actual = j.Agent
			}
			if actual == agent {
				filtered = append(filtered, j)
			}
		}
		jobs = filtered
	}

	// Filter by since (ISO 8601 timestamp)
	if since := q.Get("since"); since != "" {
		sinceTime, err := time.Parse(time.RFC3339, since)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid 'since' parameter: "+err.Error())
			return
		}
		var filtered []*job.State
		for _, j := range jobs {
			if t, err := time.Parse(time.RFC3339, j.CreatedAt); err == nil && !t.Before(sinceTime) {
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
	serveOutputFile(w, r, job.OutputPath(id), "output not found for job: "+id)
}
