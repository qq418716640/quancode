package router

import (
	"sort"
	"strings"

	"github.com/qq418716640/quancode/config"
)

type Selection struct {
	AgentKey string
	Reason   string
}

// SelectAgent picks the best non-primary agent for the given task.
// Rules: 1) preferred_for keyword match  2) priority  3) alphabetical
func SelectAgent(cfg *config.Config, task string) *Selection {
	taskLower := strings.ToLower(task)

	type candidate struct {
		key            string
		ac             config.AgentConfig
		score          int
		matchedKeyword string
	}

	var candidates []candidate
	for key, ac := range cfg.Agents {
		if !ac.Enabled || key == cfg.DefaultPrimary {
			continue
		}
		c := candidate{key: key, ac: ac}

		// Check preferred_for keyword match
		for _, keyword := range ac.PreferredFor {
			if strings.Contains(taskLower, strings.ToLower(keyword)) {
				c.score = 100
				c.matchedKeyword = keyword
				break
			}
		}

		candidates = append(candidates, c)
	}

	if len(candidates) == 0 {
		return nil
	}

	// Sort: higher score first, then lower priority number, then alphabetical
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].ac.Priority != candidates[j].ac.Priority {
			return candidates[i].ac.Priority < candidates[j].ac.Priority
		}
		return candidates[i].key < candidates[j].key
	})

	best := candidates[0]
	reason := "lowest priority number"
	if best.score > 0 {
		reason = "preferred_for keyword match: " + best.matchedKeyword
	}

	return &Selection{
		AgentKey: best.key,
		Reason:   reason,
	}
}
