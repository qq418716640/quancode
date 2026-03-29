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

- scans `PATH` for known coding CLIs such as `claude`, `codex`, and `qodercli`
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
quancode delegate --agent qoder "review this code for issues"
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

### Context Injection

QuanCode can attach project context automatically when it delegates a task.

By default, it injects `CLAUDE.md` and `AGENTS.md` if those files exist in the target working tree.

You can add more context files explicitly:

```bash
quancode delegate --context-files docs/architecture.md "explain the runner package"
quancode delegate --context-files docs/architecture.md --context-files README.md "update the config docs"
```

You can also attach a git diff snapshot:

```bash
quancode delegate --context-diff staged "review the staged changes"
quancode delegate --context-diff working "summarize the current uncommitted edits"
```

Override the context size budget when needed:

```bash
quancode delegate --context-max-size 65536 "analyze the current project instructions"
```

Disable automatic context injection entirely:

```bash
quancode delegate --no-context "review this patch for regressions"
```

Context rules:

- automatic files are `CLAUDE.md` and `AGENTS.md` when present
- `--context-files` may be passed multiple times
- `--context-diff` accepts `staged` or `working`
- `--context-max-size` overrides the total context budget
- `--no-context` disables automatic injection
- default total budget is 32 KB
- default per-file limit is 16 KB

### Post-Delegation Verification

QuanCode can run verification commands after a successful delegation.

Use `--verify` to record verification results without changing the delegation outcome:

```bash
quancode delegate --verify "go test ./..." "add tests for config loading"
```

Use `--verify-strict` when verification must pass:

```bash
quancode delegate --isolation worktree --verify-strict "go test ./..." "implement the helper and update tests"
```

Set a per-command timeout when verification may take longer or should fail faster:

```bash
quancode delegate --verify "go test ./..." --verify-timeout 300 "refactor the router package"
```

Verification rules:

- `--verify` records verification results but does not block a successful delegation
- `--verify-strict` fails the delegation when verification fails
- `--verify-timeout` sets the timeout for each verification command and defaults to 120 seconds
- `--verify` and `--verify-strict` are mutually exclusive
- verification only runs when the agent task itself succeeds
- in `worktree` mode, verification runs before the patch is applied back to the main tree
- verification failure does not trigger fallback

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

### Parallel Delegation

You can run multiple delegates concurrently using `--isolation patch` mode. Each delegate works in its own isolated worktree, and patches are not auto-applied.

```bash
# Run two delegates in parallel (from a script or an agent that supports concurrent calls)
quancode delegate --agent codex --isolation patch --format json "implement feature X in pkg/foo"
quancode delegate --agent codex --isolation patch --format json "write tests for pkg/bar"
```

The JSON result includes a `patch` field with the unified diff. To apply a patch:

```bash
quancode apply-patch --workdir /path/to/repo --file /tmp/patch-feature.diff
```

Or pipe from stdin:

```bash
echo "$PATCH" | quancode apply-patch --workdir /path/to/repo
```

`apply-patch` prints a summary of affected files before applying, so you can review what will change. Apply patches one at a time and verify after each one.

Split parallel tasks by file boundaries to avoid patch conflicts.

### End-to-End Delegation Example

```bash
quancode delegate --agent codex --isolation worktree --format text "write tests for router selection"
```

Typical flow:

- QuanCode starts the selected sub-agent in the chosen execution mode
- the sub-agent performs the one-shot task
- QuanCode returns the result and records it in the local ledger
- `changed_files` and timing data are available for later stats tracking

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
- per-agent success rate, failures, timeouts, average time, total time, and changed file counts

If no ledger data exists yet, `stats` tells you to run `quancode delegate` first.

### Statusline

`quancode init` auto-configures the Claude Code statusline. Once configured, the statusline shows:

- QuanCode session indicator and current model
- Context window usage percentage
- 5-hour and 7-day rate limit consumption
- Accumulated cost for the current session

No extra setup is needed beyond running `quancode init`.

### `quancode version`

Print the currently installed version:

```bash
quancode version
```

## 6. Auto-Fallback

When a delegated task fails due to a **timeout** or **rate-limit** error, QuanCode automatically retries the task with the next available agent according to router priority. This is called auto-fallback.

Key behaviors:

- Fallback **triggers** on timeout or rate-limit errors only.
- Fallback does **not** trigger on normal task failures (e.g., the agent ran but produced incorrect output or exited with an error).
- Fallback does **not** trigger on verification failures.
- The next agent is selected by the router using the same priority rules as normal routing.
- QuanCode attempts a maximum of **3 attempts** (the original plus up to 2 fallbacks).
- In `inplace` isolation mode, fallback is **blocked** if the failed agent already changed files in the working tree. This prevents a second agent from building on top of a partial or broken edit.

To disable fallback entirely for a delegation:

```bash
quancode delegate --no-fallback "migrate the database schema"
```

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

### File-based prompt injection did not restore cleanly

This should be rare. If it happens:

- inspect the affected file such as `AGENTS.md`
- compare it with your last committed or expected content
- rerun `quancode start` only after understanding the mismatch

### Delegating to Claude fails with "Not logged in" from third-party desktop apps

Claude Code stores authentication in the macOS Keychain. Third-party desktop apps (Codex Desktop, Qoder Desktop, etc.) may not have Keychain access, causing `claude auth status` to return `loggedIn: false` even though Claude Code works fine in the terminal.

This is a platform limitation, not a QuanCode issue. Workarounds:

- From Codex/Qoder Desktop, delegate to codex or qoder instead of claude
- From Claude Code terminal or Claude Desktop, all agents work normally
- Alternatively, set `ANTHROPIC_API_KEY` in the claude agent's `env` config to bypass Keychain auth (uses API billing, not subscription)

### Stats look wrong or empty

`quancode stats` reads local JSONL ledger files under `~/.config/quancode/logs`.

If you recently cleared that directory, the stats view starts from a fresh baseline.

## 9. The `/quancode` Skill

QuanCode ships a Claude Code skill that lets Claude Desktop (Code mode) and Dispatch orchestrate sub-agent delegation without leaving the conversation.

### Installation

Copy or symlink the skill directory into your Claude Code skills folder:

```bash
# symlink
ln -s /path/to/QuanCode/skills/quancode ~/.claude/skills/quancode

# or copy
cp -r /path/to/QuanCode/skills/quancode ~/.claude/skills/quancode
```

Once installed, Claude Code recognizes the `/quancode` slash command and can route coding tasks to any enabled QuanCode agent through `quancode delegate`.

### Usage

The skill works with:

- **Claude Desktop Code mode** — invoke `/quancode` in conversation to delegate a bounded coding task.
- **Dispatch** — use the skill as part of a multi-agent workflow where Claude Code acts as the orchestrator and QuanCode agents handle implementation.
