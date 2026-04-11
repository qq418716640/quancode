package server

import (
	"net/http"
	"sort"

	"github.com/qq418716640/quancode/config"
)

type agentInfo struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Description string   `json:"description,omitempty"`
	Strengths   []string `json:"strengths,omitempty"`
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if s.demoMode {
		writeJSON(w, http.StatusOK, map[string]any{"agents": demoAgentInfoList})
		return
	}

	cfg, err := config.Load("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}

	var agents []agentInfo
	for key, ac := range cfg.Agents {
		if !ac.Enabled {
			continue
		}
		agents = append(agents, agentInfo{
			Key:         key,
			Name:        ac.Name,
			Enabled:     ac.Enabled,
			Description: ac.Description,
			Strengths:   ac.Strengths,
		})
	}

	sort.Slice(agents, func(i, j int) bool { return agents[i].Key < agents[j].Key })

	writeJSON(w, http.StatusOK, map[string]any{
		"agents": agents,
	})
}
