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

// SelectAgentExcluding picks the best non-primary agent, skipping any in the exclude set.
func SelectAgentExcluding(cfg *config.Config, task string, exclude map[string]bool) *Selection {
	taskLower := strings.ToLower(task)

	type candidate struct {
		key            string
		ac             config.AgentConfig
		score          int
		matchedKeyword string
	}

	var candidates []candidate
	for key, ac := range cfg.Agents {
		if !ac.Enabled || key == cfg.DefaultPrimary || exclude[key] {
			continue
		}
		c := candidate{key: key, ac: ac}
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
	reason := "fallback: lowest priority number"
	if best.score > 0 {
		reason = "fallback: preferred_for keyword match: " + best.matchedKeyword
	}
	return &Selection{AgentKey: best.key, Reason: reason}
}

// HealthFilter reports whether automatic routing should currently skip an
// agent. *health.Snapshot implements it. Kept as an interface so the router
// stays a pure function of config plus an explicit exclusion rule.
type HealthFilter interface {
	IsOpen(agent string) (bool, string)
}

// ProbePreferrer picks which unhealthy agent to force-probe when every
// candidate is unhealthy. Filters that do not implement it fall back to the
// ordinary preferred_for/priority ordering.
type ProbePreferrer interface {
	PreferredProbe(candidates []string) string
}

// SelectHealthy is SelectAgentExcluding with unhealthy agents filtered out.
//
// allowProbe controls what happens when every remaining candidate is
// unhealthy: true returns the best of them anyway (with probed=true), false
// returns nil. Callers running a chain should pass true once and false
// afterwards, so a fallback chain probes a broken agent at most one time
// instead of marching through every one of them.
func SelectHealthy(cfg *config.Config, task string, exclude map[string]bool, hf HealthFilter, allowProbe bool) (sel *Selection, probed bool, skipped map[string]string) {
	if hf == nil {
		return SelectAgentExcluding(cfg, task, exclude), false, nil
	}

	// Widen the exclusion set with everything the breaker has open.
	widened := make(map[string]bool, len(exclude)+len(cfg.Agents))
	for k, v := range exclude {
		widened[k] = v
	}
	skipped = map[string]string{}
	for key := range cfg.Agents {
		if widened[key] {
			continue
		}
		if open, reason := hf.IsOpen(key); open {
			widened[key] = true
			skipped[key] = reason
		}
	}

	if sel = SelectAgentExcluding(cfg, task, widened); sel != nil {
		return sel, false, skipped
	}
	if !allowProbe || len(skipped) == 0 {
		return nil, false, skipped
	}
	// Everything is unhealthy. Fail open with a single probe rather than
	// leaving the caller with no agent at all. Prefer the agent closest to
	// recovering over whatever priority order would have picked.
	if pp, ok := hf.(ProbePreferrer); ok {
		var candidates []string
		for key := range skipped {
			candidates = append(candidates, key)
		}
		sort.Strings(candidates) // deterministic input for the picker
		if best := pp.PreferredProbe(candidates); best != "" {
			ac, known := cfg.Agents[best]
			if known && ac.Enabled && best != cfg.DefaultPrimary && !exclude[best] {
				return &Selection{AgentKey: best, Reason: "forced probe: closest to recovery"}, true, skipped
			}
		}
	}
	return SelectAgentExcluding(cfg, task, exclude), true, skipped
}
