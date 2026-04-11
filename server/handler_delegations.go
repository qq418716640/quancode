package server

import (
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/qq418716640/quancode/ledger"
)

func (s *Server) handleDelegations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// Time filter
	var entries []ledger.Entry
	var err error
	if sinceStr := q.Get("since"); sinceStr != "" {
		since, parseErr := time.Parse(time.RFC3339, sinceStr)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid 'since' parameter: "+parseErr.Error())
			return
		}
		entries, err = s.readEntriesSince(since)
	} else {
		entries, err = s.readAllEntries()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read ledger: "+err.Error())
		return
	}

	// Filter by agent
	if agent := q.Get("agent"); agent != "" {
		entries = filterEntries(entries, func(e ledger.Entry) bool { return e.Agent == agent })
	}

	// Filter by status
	if status := q.Get("status"); status != "" {
		entries = filterEntries(entries, func(e ledger.Entry) bool {
			if status == "succeeded" {
				return e.ExitCode == 0
			}
			if status == "failed" {
				return e.ExitCode != 0 && !e.TimedOut
			}
			if status == "timed_out" {
				return e.TimedOut
			}
			return e.FinalStatus == status
		})
	}

	total := len(entries)

	// Reverse to show newest first
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	// Pagination
	limit := 100
	if l := q.Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}
	offset := 0
	if o := q.Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	if offset > len(entries) {
		offset = len(entries)
	}
	end := offset + limit
	if end > len(entries) {
		end = len(entries)
	}
	page := entries[offset:end]

	writeJSON(w, http.StatusOK, map[string]any{
		"total":   total,
		"entries": page,
	})
}

// validDelegationID matches delegation IDs like "del_9b03d6fbacb19ba8".
var validDelegationID = regexp.MustCompile(`^del_[a-f0-9]{16}$`)

func (s *Server) handleDelegationOutput(w http.ResponseWriter, r *http.Request) {
	if s.demoMode {
		writeError(w, http.StatusNotFound, "output not available in demo mode")
		return
	}
	id := r.PathValue("id")
	if id == "" || !validDelegationID.MatchString(id) {
		writeError(w, http.StatusBadRequest, "invalid delegation id")
		return
	}
	serveOutputFile(w, r, ledger.OutputPath(id), "output not found for delegation: "+id)
}

func filterEntries(entries []ledger.Entry, fn func(ledger.Entry) bool) []ledger.Entry {
	var result []ledger.Entry
	for _, e := range entries {
		if fn(e) {
			result = append(result, e)
		}
	}
	return result
}
