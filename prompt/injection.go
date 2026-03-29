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
- QuanCode automatically injects project context (CLAUDE.md, AGENTS.md) into every delegation. You do NOT need to copy these files into the task description. Focus on WHAT to do and WHY.
- If the sub-agent needs specific source files for context, use --context-files:
    {{.Binary}} delegate --agent codex --context-files "router/router.go" --context-files "router/router_test.go" "add tests for SelectAgentExcluding"
- Use --context-diff staged or --context-diff working to include uncommitted changes when relevant.
- Use --no-context to disable automatic context injection if the task is self-contained.
- The sub-agent CANNOT see your conversation history. Your task description + injected context is all it gets. Be specific about:
  - What to do and why
  - Which files, functions, or symbols are involved
  - Constraints, non-goals, and acceptance criteria
  - Good: "Add unit tests for router/router.go SelectAgent — cover: no match returns nil, keyword match beats priority, exclude list is respected. Do not modify production code."
  - Bad: "Write tests for the router changes we discussed."
- After delegation completes, check changed_files in the JSON result and verify the changes
- Do NOT delegate tasks that require multi-step conversation or clarification from the user
- Do NOT delegate if you can do the task yourself just as efficiently
- You are the primary agent. You own the overall plan and final quality.

TASK TYPES — match your task description to the type:
- Code modification: specify files, functions, constraints, acceptance criteria. Add --verify for automated checks.
- Research/analysis (e.g., "review this code", "evaluate this design"): clearly state WHAT to analyze, WHAT output format you expect (e.g., "list of issues", "comparison table"), and explicitly say "DO NOT write code" if the task is analysis-only. Keep the scope narrow — broad questions like "research everything about X" will time out or produce unfocused results.
- Code review: provide the diff or changed files via --context-diff, state what aspects to review (correctness, security, performance, style).

ISOLATION MODES:
- Single task: use --isolation worktree for safe isolated execution with automatic patch application.
- Multiple parallel tasks: use --isolation patch --format json to collect patches without auto-applying.
- Default (inplace): runs directly in the working directory. Use only when isolation is unnecessary.

VERIFICATION:
- For code modification tasks, add --verify to run a check after the sub-agent finishes:
    {{.Binary}} delegate --agent codex --isolation worktree --verify "go test ./affected/package" "fix the bug in parser"
- The verify command runs in the worktree before applying changes. If it fails, you'll see the failure in the result but changes are still applied (use --verify-strict to block application on failure).

PARALLEL DELEGATION:
You can run multiple delegate calls concurrently for independent tasks.
- MUST use --isolation patch --format json for each parallel delegate to avoid conflicts.
- Patches are automatically cached. To apply, use the delegation_id from the JSON result:
    {{.Binary}} apply-patch --id <delegation_id>
- Split tasks by file boundaries — avoid two delegates modifying the same file.
- Apply patches one at a time. If a patch conflicts, resolve before applying the next one.`

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
