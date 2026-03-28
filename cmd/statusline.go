package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const statuslineScript = `#!/bin/bash
input=$(cat)
model=$(echo "$input" | jq -r '.model.display_name // empty')
ctx=$(echo "$input" | jq -r '.context_window.used_percentage // empty')
r5h=$(echo "$input" | jq -r '.rate_limits.five_hour.used_percentage // empty')
r7d=$(echo "$input" | jq -r '.rate_limits.seven_day.used_percentage // empty')
cost=$(echo "$input" | jq -r '.cost.total_cost_usd // empty')

parts=""
if [ "$QUANCODE_SESSION" = "1" ]; then
    parts="⚡QuanCode"
    [ -n "$QUANCODE_PRIMARY" ] && parts="$parts:$QUANCODE_PRIMARY"
fi
[ -n "$model" ] && parts="$parts${parts:+ | }$model"
[ -n "$ctx" ] && parts="$parts ctx:${ctx}%"
[ -n "$r5h" ] && parts="$parts 5h:${r5h}%"
[ -n "$r7d" ] && parts="$parts 7d:${r7d}%"
[ -n "$cost" ] && [ "$cost" != "0" ] && parts="$parts \$${cost}"

echo "$parts"
`

// setupClaudeStatusLine installs the quancode statusline script for Claude Code
// and configures it in Claude Code's settings.json if not already set.
func setupClaudeStatusLine() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	claudeDir := filepath.Join(home, ".claude")
	scriptPath := filepath.Join(claudeDir, "statusline.sh")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Write statusline script
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(scriptPath, []byte(statuslineScript), 0755); err != nil {
		return err
	}
	fmt.Printf("[quancode] statusline script written to %s\n", scriptPath)

	// Read existing settings or start fresh
	var settings map[string]interface{}
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	if settings == nil {
		settings = make(map[string]interface{})
	}

	// Only set statusLine if not already configured
	if _, exists := settings["statusLine"]; exists {
		fmt.Println("[quancode] statusLine already configured in Claude Code settings, skipping")
		return nil
	}

	settings["statusLine"] = map[string]interface{}{
		"type":    "command",
		"command": "~/.claude/statusline.sh",
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		return err
	}
	fmt.Printf("[quancode] statusLine configured in %s\n", settingsPath)
	return nil
}
