package ledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// QuotaConfig stores quota settings for all agents.
// Each agent can have multiple quota rules (e.g. a 5-hour rolling window AND a weekly cap).
type QuotaConfig struct {
	Agents map[string][]AgentQuota `json:"agents"`
}

// AgentQuota stores quota limits and reset info for one quota rule.
type AgentQuota struct {
	// Name identifies this rule when an agent has multiple quotas (e.g. "5h-window", "weekly-cap").
	Name string `json:"name,omitempty"`
	// Unit: "calls" (default), "minutes", "hours"
	Unit string `json:"unit"`
	// Limit per period (0 = unlimited)
	Limit int `json:"limit"`
	// ResetMode: "monthly" (default), "weekly", "rolling_hours"
	ResetMode string `json:"reset_mode"`
	// ResetDay: day of month (1-28) for monthly, day of week (1=Mon..7=Sun) for weekly
	ResetDay int `json:"reset_day"`
	// RollingHours: window size for rolling_hours mode (e.g. 5 for Claude Max 5-hour window)
	RollingHours int `json:"rolling_hours,omitempty"`
	// Notes: human-readable description
	Notes string `json:"notes"`
}

// Usage calculates current usage for an agent based on ledger entries.
func (aq *AgentQuota) Usage(agentKey string) (used float64, periodStart time.Time) {
	since := aq.PeriodStart()
	entries, _ := ReadSince(since)

	var total float64
	for _, e := range entries {
		if e.Agent != agentKey {
			continue
		}
		switch aq.effectiveUnit() {
		case "calls":
			total++
		case "minutes":
			total += float64(e.DurationMs) / 60000
		case "hours":
			total += float64(e.DurationMs) / 3600000
		}
	}
	return total, since
}

// PeriodStart returns when the current quota period began.
func (aq *AgentQuota) PeriodStart() time.Time {
	now := time.Now()
	mode := aq.effectiveResetMode()

	switch mode {
	case "weekly":
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7 // Sunday = 7
		}
		resetDay := aq.ResetDay
		if resetDay < 1 || resetDay > 7 {
			resetDay = 1
		}
		daysBack := weekday - resetDay
		if daysBack < 0 {
			daysBack += 7
		}
		start := now.AddDate(0, 0, -daysBack)
		return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, now.Location())

	case "rolling_hours":
		hours := aq.RollingHours
		if hours <= 0 {
			hours = 5 // default 5-hour window
		}
		return now.Add(-time.Duration(hours) * time.Hour)

	default: // "monthly"
		resetDay := aq.ResetDay
		if resetDay < 1 || resetDay > 28 {
			resetDay = 1
		}
		year, month, day := now.Date()
		if day >= resetDay {
			return time.Date(year, month, resetDay, 0, 0, 0, 0, now.Location())
		}
		return time.Date(year, month-1, resetDay, 0, 0, 0, 0, now.Location())
	}
}

func (aq *AgentQuota) effectiveUnit() string {
	if aq.Unit == "" {
		return "calls"
	}
	return aq.Unit
}

func (aq *AgentQuota) effectiveResetMode() string {
	if aq.ResetMode == "" {
		return "monthly"
	}
	return aq.ResetMode
}

func quotaFilePath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "quancode", "quota.json")
	}
	return filepath.Join(".", ".quancode", "quota.json")
}

// LoadQuota loads the quota config from disk.
// Handles both the current format (agents: map[string][]AgentQuota)
// and the legacy format (agents: map[string]AgentQuota) transparently.
func LoadQuota() (*QuotaConfig, error) {
	data, err := os.ReadFile(quotaFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return &QuotaConfig{Agents: make(map[string][]AgentQuota)}, nil
		}
		return nil, fmt.Errorf("read quota file: %w", err)
	}

	// Try current format first.
	var qc QuotaConfig
	if err := json.Unmarshal(data, &qc); err == nil && qc.Agents != nil {
		return &qc, nil
	}

	// Fall back to legacy format (single AgentQuota per agent).
	var legacy struct {
		Agents map[string]AgentQuota `json:"agents"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("parse quota file: %w", err)
	}
	migrated := &QuotaConfig{Agents: make(map[string][]AgentQuota, len(legacy.Agents))}
	for key, aq := range legacy.Agents {
		if aq.Name == "" {
			aq.Name = "default"
		}
		migrated.Agents[key] = []AgentQuota{aq}
	}
	return migrated, nil
}

// SaveQuota writes the quota config to disk atomically (temp file + rename).
func SaveQuota(qc *QuotaConfig) error {
	target := quotaFilePath()
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(qc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal quota: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "quota-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync temp file: %w", err)
	}
	tmp.Close()

	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
