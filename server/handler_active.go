package server

import (
	"net/http"

	"github.com/qq418716640/quancode/active"
)

func (s *Server) handleActive(w http.ResponseWriter, r *http.Request) {
	entries := active.List()
	if entries == nil {
		entries = []active.Entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tasks": entries,
	})
}
