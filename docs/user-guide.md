# User Guide

This guide covers the day-to-day command flow for QuanCode after installation.

README stays focused on project overview and quick start. This guide explains how to actually use each command in a normal workflow.

## 1. Setup

### `quancode init`

Use `init` the first time you install QuanCode:

```bash
quancode init
```

What it does:

- scans `PATH` for known coding CLIs such as `claude`, `codex`, `aider`, and `opencode`
- asks which detected CLI should be the default primary agent
- writes a config file to `~/.config/quancode/quancode.yaml`

If the config file already exists, `init` asks before overwriting it.

### `quancode doctor`

Run `doctor` after `init` or after changing your setup:

```bash
quancode doctor
```

It checks:

- whether the config file exists
- whether the config loads and validates
- whether the primary agent command is available in `PATH`
- whether each enabled agent command is available
- whether `quancode` itself is in `PATH`

If a check fails, `doctor` prints a short hint for the next step. It may also print a shell-completion setup tip for your current shell.

### Shell Completion

QuanCode already includes shell completion generation through Cobra:

```bash
quancode completion zsh
quancode completion bash
quancode completion fish
```

Quick setup examples:

```bash
# zsh
echo 'source <(quancode completion zsh)' >> ~/.zshrc

# bash
echo 'source <(quancode completion bash)' >> ~/.bashrc

# fish
quancode completion fish > ~/.config/fish/completions/quancode.fish
```

Open a new shell after adding the command, or source the generated completion script in your current session.

If you installed QuanCode through Homebrew after the tap is wired, the generated formula installs shell completions automatically.

## 2. Starting a Primary Agent

### `quancode start`

Start the configured default primary agent:

```bash
quancode start
```

Override the primary agent for a single session:

```bash
quancode start --primary codex
quancode start --primary claude
```

What happens when `start` runs:

- QuanCode loads your config
- it builds delegation instructions listing the enabled non-primary agents
- it injects those instructions using the configured prompt mode for the primary CLI
- it launches the primary CLI

If the primary uses file-based prompt injection such as `AGENTS.md`, QuanCode manages that file for the session and restores the original content when the primary exits.

## 3. Delegation

### `quancode delegate`

Use `delegate` for one-shot sub-agent work:

```bash
quancode delegate "write unit tests for config loading"
```

Target a specific agent:

```bash
quancode delegate --agent claude "review this patch for regressions"
quancode delegate --agent codex "refactor this helper and update tests"
```

Change the working directory:

```bash
quancode delegate --workdir /path/to/repo "explain the runner package"
```

Choose output format:

```bash
quancode delegate --format text "summarize the repo structure"
quancode delegate --format json "review this change"
```

Text mode is easier for direct use in the terminal. JSON mode is better for scripts and automation.

### Isolation Modes

QuanCode supports three delegation modes:

- `inplace`: runs directly in your current working tree
- `worktree`: runs in a temporary git worktree, then applies the resulting patch back
- `patch`: runs in a temporary git worktree and returns a patch instead of changing your main tree

Examples:

```bash
quancode delegate --isolation inplace "fix this lint issue"
quancode delegate --isolation worktree "implement the helper and update tests"
quancode delegate --isolation patch "rewrite the README opening paragraph"
```

Use this rule of thumb:

- use `inplace` for read-only tasks or low-risk edits
- use `worktree` when you want safer execution but still want the result applied automatically
- use `patch` when you want to inspect or apply the change yourself

`worktree` and `patch` require the target directory to be a git repository.

### End-to-End Delegation Example

```bash
quancode delegate --agent codex --isolation worktree --format text "write tests for router selection"
```

Typical flow:

- QuanCode starts the selected sub-agent in the chosen execution mode
- the sub-agent performs the one-shot task
- QuanCode returns the result and records it in the local ledger
- `changed_files` and timing data are available for later stats and quota tracking

## 4. Routing

### `quancode route`

Use `route` to preview automatic selection before delegating:

```bash
quancode route "review this Go patch"
quancode route "implement a new command and update docs"
```

Output includes:

- the original task text
- the selected agent
- the reason for the selection

Routing is keyword and priority based. It is not an LLM planner.

## 5. Observability

### `quancode agents`

List enabled agents and whether each command is available:

```bash
quancode agents
```

The output includes agent name, availability, command, strengths, and description.

### `quancode stats`

Show recent delegation statistics:

```bash
quancode stats
quancode stats --days 7
```

The stats view includes:

- total calls in the selected window
- per-agent success rate, failures, timeouts, average time, total time, changed file counts, and approval counts
- an approval summary when approval requests occurred during that period

If no ledger data exists yet, `stats` tells you to run `quancode delegate` first.

### `quancode quota`

View configured quota limits:

```bash
quancode quota
```

Set a quota for a specific agent:

```bash
quancode quota --set-agent claude --unit hours --limit 5 --reset-mode rolling_hours --rolling-hours 5 --notes "Claude Max"
quancode quota --set-agent codex --unit calls --limit 200 --reset-mode weekly --reset-day 1 --notes "Codex Pro"
```

Supported units:

- `calls`
- `minutes`
- `hours`

Supported reset modes:

- `monthly`
- `weekly`
- `rolling_hours`

The quota view shows current usage and remaining budget in the active period.

### `quancode version`

Print the currently installed version:

```bash
quancode version
```

## 6. Approval Flow

Some delegated tasks may request approval before continuing.

When that happens, QuanCode prints a request id and an approval command such as:

```bash
quancode approve req_123456 --allow --approval-dir /path/to/approval-dir
```

Approve a request:

```bash
quancode approve req_123456 --allow --approval-dir /path/to/approval-dir
```

Deny a request:

```bash
quancode approve req_123456 --deny --approval-dir /path/to/approval-dir --reason "do not push from this machine"
```

If `--approval-dir` is omitted, `approve` falls back to the `QUANCODE_APPROVAL_DIR` environment variable.

Important:

- approval requests currently time out after 120 seconds
- when that timeout is reached without a response, QuanCode records an automatic deny decision
- if you see an approval prompt, handle it promptly in another terminal

## 7. Configuration Recipes

For the full field reference, see [`agent-config-schema.md`](agent-config-schema.md).

### Start from the example config

```bash
cp quancode.example.yaml ~/.config/quancode/quancode.yaml
```

Then edit the primary agent and enabled agents for your machine.

### Add a custom agent

Add a new entry under `agents`:

```yaml
agents:
  mycli:
    name: My CLI
    command: mycli
    enabled: true
    priority: 25
    strengths: ["review", "tests"]
    delegate_args: ["run"]
```

QuanCode treats adapters as data-driven config. You do not need to change Go code just to describe another CLI shape.

### Set per-agent environment variables

Use the `env` field when a specific agent needs extra environment variables:

```yaml
agents:
  codex:
    command: codex
    enabled: true
    env:
      HTTPS_PROXY: http://127.0.0.1:7890
```

### Adjust timeouts

Increase or decrease the per-agent timeout with `timeout_secs`:

```yaml
agents:
  claude:
    command: claude
    enabled: true
    timeout_secs: 600
```

## 8. Troubleshooting

### `doctor` fails on config or missing commands

Run:

```bash
quancode doctor
```

Then fix the first reported failure before moving on to later ones.

### Delegation times out

Check:

- whether the target CLI is installed and logged in
- whether the task is too broad for a one-shot delegate call
- whether the agent timeout is too low
- whether the delegate command is waiting for approval

### File-based prompt injection did not restore cleanly

This should be rare. If it happens:

- inspect the affected file such as `AGENTS.md`
- compare it with your last committed or expected content
- rerun `quancode start` only after understanding the mismatch

### Stats look wrong or empty

`quancode stats` reads local JSONL ledger files under `~/.config/quancode/logs`.

If you recently cleared that directory, the stats view starts from a fresh baseline.
