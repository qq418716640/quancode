package prompt

import (
	"bytes"
	"sort"
	"strings"
	"text/template"

	"github.com/qq418716640/quancode/config"
)

const promptTemplate = `You have access to additional AI coding agents through quancode.

AVAILABLE AGENTS:
{{- range .Agents}}
- {{.Name}} ("{{.Key}}"): {{.Description}}
  Strengths: {{.Strengths}}
{{- end}}

TO DELEGATE A TASK:
  {{.Binary}} delegate --agent <agent-name> --format json --workdir "$(pwd)" "<detailed task description>"

The JSON result contains:
  {"agent":"...", "task":"...", "exit_code":0, "timed_out":false, "duration_ms":1234, "output":"...", "changed_files":["file1.go","file2.go"]}

Use --format text instead if you only need the raw output.

TO LIST AVAILABLE AGENTS:
  {{.Binary}} agents

DELEGATION GUIDELINES:
- ALWAYS use "{{.Binary}} delegate" to invoke other agents. NEVER call their CLI commands directly (e.g., do NOT run "claude -p ..." or "codex exec ..." yourself). QuanCode manages authentication, proxy, and environment for each agent.
- Delegate well-scoped, independent tasks (e.g., "write tests for X", "refactor file Y")
- Provide full context in the task description: what files to look at, what the goal is, constraints
- The delegate agent operates in the same working directory and can read/write files
- After delegation completes, check changed_files in the JSON result and verify the changes
- Do NOT delegate tasks that require multi-step conversation or clarification from the user
- Do NOT delegate if you can do the task yourself just as efficiently
- You are the primary agent. You own the overall plan and final quality.`

type agentInfo struct {
	Key         string
	Name        string
	Description string
	Strengths   string
}

// Build renders the system prompt to inject into the primary CLI.
// primaryKey is the agent key of the actual primary (may differ from cfg.DefaultPrimary).
// binaryPath is the path to the quancode executable.
func Build(cfg *config.Config, primaryKey, binaryPath string) (string, error) {
	var keys []string
	for key := range cfg.Agents {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var agents []agentInfo
	for _, key := range keys {
		ac := cfg.Agents[key]
		if !ac.Enabled || key == primaryKey {
			continue
		}
		agents = append(agents, agentInfo{
			Key:         key,
			Name:        ac.Name,
			Description: ac.Description,
			Strengths:   strings.Join(ac.Strengths, ", "),
		})
	}

	tmpl, err := template.New("prompt").Parse(promptTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]any{"Agents": agents, "Binary": binaryPath})
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
