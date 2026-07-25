# User Guide

QuanCode is a multi-agent orchestration layer. **You command, AIs execute.** The vast majority of QuanCode commands are called autonomously by the AI agent — you only need to learn two commands to get started.

## How It Works

```
You (natural language) → Primary Agent (AI) → quancode delegate/route/job/... → Sub-Agents
```

1. You run `quancode start` to launch a primary AI agent (e.g. Claude Code)
2. You describe what you want in natural language
3. The primary agent autonomously decides when and how to delegate tasks to other agents using `quancode delegate`, `quancode pipeline`, etc.
4. QuanCode handles routing, fallback, isolation, verification, and result collection — all transparently

**You never need to call `quancode delegate` yourself.** The AI does it for you.

## Commands You Need to Learn

### `quancode init` — one-time setup

```bash
quancode init
```

Run this once after installation. It scans your `PATH` for known coding CLIs (Claude Code, Codex, Qoder, Gemini, Copilot, etc.), lets you pick a default primary agent, and writes `~/.config/quancode/quancode.yaml`.

### `quancode start` — start a session

```bash
quancode start
```

This is the command you use every day. It launches the primary AI agent with multi-agent delegation capabilities injected. From here, just talk to the AI.

Override the primary for a single session:

```bash
quancode start --primary codex
```

**That's it.** These two commands cover 95% of daily usage. Everything below is reference for what the AI does under the hood, and optional tools for power users.

---

## What the AI Does Autonomously

When you give the primary agent a task, it can autonomously use any of the following QuanCode capabilities. You don't need to memorize these — the AI already knows how to use them.

### Delegation (`quancode delegate`)

The AI routes tasks to the best sub-agent:

- **Auto-routing**: picks the best agent based on task keywords and priority
- **Targeted delegation**: sends to a specific agent when appropriate
- **Context injection**: automatically attaches `CLAUDE.md`, `AGENTS.md`, and relevant files
- **Isolation modes**: `inplace` (direct edit), `worktree` (safe sandbox + auto-apply), `patch` (sandbox + manual apply)
- **Verification**: runs test commands after delegation to validate results
- **Parallel delegation**: splits independent tasks across multiple agents concurrently
- **Async delegation**: runs long tasks in the background with `--async`

### Fallback and Recovery

- **Auto-fallback**: if an agent times out or hits rate limits, QuanCode automatically retries with the next available agent (up to 3 attempts)
- **Speculative parallelism**: optionally races two agents in parallel — first success wins, companion result preserved for reference
- **Isolation filtering**: agents that don't support the required isolation mode are automatically skipped

### Pipeline (`quancode pipeline`)

For multi-phase tasks (analyze → implement → test), the AI can run a pipeline where each stage's output flows to the next. Stages can have per-stage fallback, verification, and failure policies.

### Job Management (`quancode job`)

For background tasks, the AI manages the full lifecycle: launch, monitor, retrieve results, cancel, and clean up.

### Agent Health

QuanCode watches how each agent has been doing lately and stops routing work to one that is currently broken — out of quota, upstream rejecting everything, or logged out. After a few such failures in a row an agent is set aside for a while, with the wait growing longer the longer it stays broken.

This only affects automatic routing. If you name an agent explicitly with `--agent`, it still runs; you just get a warning first. And if every agent looks unhealthy, QuanCode still tries one rather than leaving you stuck.

Timeouts and ordinary task failures never count against an agent — a hard task is not a broken agent.

Run `quancode agents` to see which agents are currently set aside and for how long.

### Batch (`quancode batch`)

When you have the same task to run across many items — one per scope, per chunk, per file — `batch` runs them one at a time and remembers which ones finished.

```bash
# check what would run, without executing anything
quancode batch --template-file review.tmpl --items-file scopes.txt --dry-run

# run it
quancode batch --template-file review.tmpl --items-file scopes.txt

# pick up where it left off — finished items are not redone
quancode batch --resume batch_7f3a2b1c
```

The template is Go `text/template` with `{{.Item}}`, `{{.Index}}` (0-based), `{{.Number}}` (1-based), and `{{.Total}}`. The items file is one item per line; blank lines and `#` comments are ignored.

Things worth knowing:

- **Successful items never re-run.** Resume is safe to call repeatedly.
- **Failures are treated differently depending on why.** Timeouts and rate limits retry automatically on the next run. A task that failed for its own reasons stays failed until you pass `--retry-failed`, so a genuinely broken item does not burn quota every time you resume.
- **The template is frozen when the batch is created.** Editing your template file afterwards will not change a half-finished batch — create a new one instead.
- **`--dry-run` checks every item renders** and shows you the first and last prompt. A typo in the template fails there rather than after 900 delegations.
- **Items run one at a time.** Running them in parallel would multiply how fast a shared account hits its quota.
- **Ctrl+C stops after the current item** and prints the resume command.

`quancode batch --list` shows recent batches and their status.

## Optional Commands for Power Users

These commands are useful for debugging, monitoring, or manual intervention. You can use QuanCode productively without ever touching them.

### Health Check

```bash
quancode doctor       # verify config, agents, and PATH
```

### Observability

```bash
quancode agents       # list enabled agents and availability
quancode stats        # delegation statistics (success rate, timing, etc.)
quancode dashboard    # web UI for monitoring (preview)
quancode version      # print installed version
```

### Statusline

`quancode init` auto-configures the Claude Code statusline showing context window usage, rate limits, and session cost. No extra setup needed.

### Dashboard (preview)

```bash
quancode dashboard                # default port 8377
quancode dashboard --port 9000    # custom port
quancode dashboard --open         # auto-open browser
```

Browser-based UI with delegation history, async job status, pipeline visualization, and real-time updates via SSE. Listens on `127.0.0.1` only, read-only, no auth required. Frontend assets are embedded — no internet needed.

### Manual Delegation

In rare cases you may want to delegate directly from the terminal:

```bash
quancode delegate "write unit tests for config loading"
quancode delegate --agent codex --isolation worktree "refactor the helper"
quancode delegate --async --isolation worktree "implement feature X"
```

See the sections below for full flag reference.

### Shell Completion

```bash
quancode completion zsh   # or bash, fish
# Quick setup:
echo 'source <(quancode completion zsh)' >> ~/.zshrc
```

## Reference

The following sections document all flags and behaviors in detail. They are primarily useful for understanding what the AI is doing, writing custom configs, or troubleshooting.

### Delegation Flags

| Flag | Description |
|---|---|
| `--agent <name>` | Target a specific agent |
| `--isolation <mode>` | `inplace`, `worktree`, or `patch` |
| `--format <fmt>` | `text` or `json` |
| `--workdir <path>` | Override working directory |
| `--async` | Run in background (requires worktree/patch) |
| `--timeout <secs>` | Per-task timeout |
| `--no-fallback` | Disable auto-fallback |
| `--verify <cmd>` | Run verification after success |
| `--verify-strict <cmd>` | Fail delegation if verification fails |
| `--verify-timeout <secs>` | Timeout for verification (default 120s) |
| `--context-files <path>` | Add extra context files (repeatable) |
| `--context-diff <type>` | Attach `staged` or `working` diff |
| `--context-max-size <bytes>` | Override context budget (default 32KB) |
| `--no-context` | Disable automatic context injection |
| `--dry-run` | Preview full prompt without executing |

### Batch Flags

| Flag | Description |
|---|---|
| `--template <text>` | Inline task template |
| `--template-file <path>` | Read the template from a file |
| `--item <text>` | One item (repeatable) |
| `--items-file <path>` | One item per line; `#` comments allowed |
| `--resume <batch-id>` | Continue an existing batch |
| `--retry-failed` | Also re-run items that failed for non-transient reasons |
| `--stop-on-failure` | Halt at the first failed item |
| `--dry-run` | Validate and preview without executing |
| `--agent <name>` | Use one agent for every item (default: auto-route) |
| `--isolation <mode>` | `inplace`, `worktree`, or `patch` |
| `--timeout <secs>` | Per-item timeout |
| `--list` | List recent batches |

### Job Management

```bash
quancode job list [--workdir .]        # list jobs (newest first)
quancode job status <job_id>           # check status
quancode job result <job_id>           # get result
quancode job logs <job_id> [--tail N]  # view output
quancode job cancel <job_id>           # cancel running job
quancode job clean [--ttl 168h]        # remove expired jobs
```

For patch-mode async jobs: `quancode apply-patch --id <delegation_id>`

### Routing Preview

```bash
quancode route "review this Go patch"
```

Shows which agent would be selected and why. Useful for understanding routing decisions.

### Context Injection Rules

- Automatic files: `CLAUDE.md` and `AGENTS.md` when present
- Default total budget: 32 KB, per-file limit: 16 KB
- `--context-diff` accepts `staged` or `working`
- `--no-context` disables all automatic injection

### Isolation Mode Details

| Mode | Behavior | Git required |
|---|---|---|
| `inplace` | Runs directly in working tree | No |
| `worktree` | Temporary git worktree, auto-applies patch | Yes |
| `patch` | Temporary git worktree, returns patch only | Yes |

Rule of thumb: `inplace` for read-only/low-risk, `worktree` for safe execution with auto-apply, `patch` for manual review.

### Verification Rules

- `--verify` records results without blocking; `--verify-strict` fails on verification failure
- Mutually exclusive; only runs after agent success
- In `worktree` mode, runs before patch apply
- Does not trigger fallback

### Auto-Fallback Details

- Triggers on timeout or rate-limit only (not normal failures or verification failures)
- Max 3 attempts (original + 2 retries)
- In `inplace` mode, blocked if the failed agent already changed files
- Disable per-delegation with `--no-fallback`

## Configuration

For the full field reference, see [`agent-config-schema.md`](agent-config-schema.md).

### Start from the example config

```bash
cp quancode.example.yaml ~/.config/quancode/quancode.yaml
```

### Add a custom agent

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

QuanCode adapters are data-driven config — no Go code needed.

### Per-agent environment variables

```yaml
agents:
  codex:
    env:
      HTTPS_PROXY: http://127.0.0.1:7890
```

### Adjust timeouts

```yaml
agents:
  claude:
    timeout_secs: 600
```

## Troubleshooting

### `doctor` reports failures

Run `quancode doctor` and fix the first reported issue before moving on.

### Delegation times out

Check: is the target CLI installed and logged in? Is the task too broad? Is the timeout too low?

### File-based prompt injection not restored

Rare. Inspect the affected file (e.g. `AGENTS.md`), compare with your last commit, and only rerun `quancode start` after understanding the mismatch.

### "Not logged in" from third-party desktop apps

Claude Code auth uses macOS Keychain, which third-party desktop apps may not access. Workarounds:
- From Codex/Qoder Desktop, delegate to codex or qoder instead of claude
- From Claude Code terminal or Claude Desktop, all agents work normally
- Set `ANTHROPIC_API_KEY` in the claude agent's `env` config to bypass Keychain

### Stats empty

`quancode stats` reads `~/.config/quancode/logs/`. If cleared, stats start from a fresh baseline.

## The `/quancode` Skill

QuanCode ships a Claude Code skill for Claude Desktop (Code mode) and Dispatch.

### Installation

```bash
ln -s /path/to/QuanCode/skills/quancode ~/.claude/skills/quancode
```

### Usage

- **Claude Desktop Code mode** — invoke `/quancode` in conversation to delegate a bounded coding task.
- **Dispatch** — use as part of a multi-agent workflow where Claude Code orchestrates and QuanCode agents handle implementation.
