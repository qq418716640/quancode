# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
go build -o quancode .    # build
go test ./...             # test
go vet ./...              # lint
go install .              # install to $GOPATH/bin
```

No CGO or special build flags required.

## Architecture

QuanCode is a Go CLI that launches a primary AI coding agent and lets it delegate tasks to other CLIs as sub-agents. All CLIs use the same data-driven `genericAgent` adapter — no per-CLI Go code needed. Built-in defaults cover Claude Code, Codex CLI, and Qoder CLI.

### Package flow

```
cmd/start.go → prompt/injection.go → agent/agent.go (LaunchAsPrimary)
cmd/delegate.go → cmd/delegate_attempt.go → router/router.go → agent/agent.go (Delegate) → runner/
                  cmd/fallback.go (auto-retry)                                              → ledger/
cmd/delegate_async.go → job/ (write pending) → cmd/run_job.go (detached) → delegate_attempt.go
cmd/job*.go → job/ (list/status/result/logs/cancel/clean)
```

### Key packages

- **agent/** — Single `genericAgent` struct implements the `Agent` interface for any CLI. Behavior is driven by config fields (`PromptMode`, `TaskMode`, `OutputMode`, `DelegateArgs`, `OutputFlag`, `Env`). Adding a new CLI means adding config, not Go code.
- **config/** — YAML config with search order: `--config` flag (must exist) > `./quancode.yaml` > `~/.config/quancode/quancode.yaml` > built-in defaults. `applyKnownAgentDefaults()` backfills newer fields into older config files for backward compatibility.
- **context/** — Builds delegation context bundles by auto-injecting project instruction files such as `CLAUDE.md` and `AGENTS.md`, with support for explicit files, git diff injection, size budgets, and path safety checks.
- **prompt/** — Builds the system prompt injected into the primary CLI. Uses `text/template`. Excludes the actual primary from the listed agents.
- **router/** — `SelectAgent()` picks the best sub-agent: preferred_for keyword match > priority number > alphabetical.
- **runner/** — Process execution with timeout, stdin piping, output file capture, env merging (`MergeEnv` replaces same-name keys, not appends). Also handles git worktree isolation and patch collection. All processes run in their own process group (`Setpgid`); timeout kills the entire group to prevent child process leaks. `RunWithContext` variants accept external contexts for speculative cancellation.
- **ledger/** — JSONL logs at `~/.config/quancode/logs/{date}.jsonl`. Records each delegation with agent, task, duration, exit code, changed files, and fallback chain. Also provides ID generation (NewDelegationID, NewRunID) for tracking.
- **job/** — Persistent async job state at `~/.config/quancode/jobs/`. Atomic writes via flock+CAS with schema versioning. Handles job lifecycle (pending→running→succeeded/failed/timed_out/cancelled/lost), TTL cleanup, PID reuse detection via `pid_start_time`, and capped output files (50MB).

### Prompt injection modes

The primary CLI receives delegation instructions via one of:
- `append_arg` — system prompt as final CLI argument (Claude Code: `--append-system-prompt`)
- `file` — inject between `<!-- quancode:begin/end -->` markers in a file (Codex: `AGENTS.md`). Original content is restored on exit via a closure returned by `injectPromptFile`. Uses `runManagedPrimary` (child process with signal forwarding) instead of `syscall.Exec` so the defer runs.
- `env` / `stdin` — via environment variable or stdin pipe

### Delegation isolation modes

`--isolation inplace` (default): run in working directory, detect changes via git status snapshot diff.
`--isolation worktree`: git worktree, collect patch, auto-apply to main directory.
`--isolation patch`: like worktree but returns patch without applying.

### Delegation verification

`--verify` records post-delegation verification results without changing a successful delegation outcome.
`--verify-strict` makes verification failure fail the delegation.
Verification only runs after the delegated agent succeeds.
In `worktree` mode, verification runs before patch apply.
Verification failure does not trigger fallback.

### Async delegation

`--async` spawns a detached `_run-job` process (Setsid) that executes the full delegation pipeline in the background. Only `worktree`/`patch` isolation allowed. The parent writes a `pending` job state, forks, and returns immediately. The runner transitions through `pending→running→terminal`, writes output/patch files, and records to ledger independently. `--verify` is not supported in async mode. `job list/status/result/logs/cancel/clean` manage the lifecycle.

### Speculative parallelism

When `preferences.speculative_delay_secs > 0` and isolation is `worktree`/`patch`, the primary agent gets a lead window. If it hasn't completed within the delay, a backup agent is launched in parallel (each in its own worktree). First success wins; the loser is cancelled via context cancellation (process group kill). Only works in synchronous mode (not `--async`). Requires fallback to be enabled. Orchestrated by `cmd/speculative.go`.

### Statusline

`quancode init` configures the Claude Code statusline to show delegation cost and rate limit usage in the terminal status bar.

## Design principles

### Core philosophy

QuanCode exists to let multiple agents collaborate on coding tasks that a single agent handles poorly or inconsistently. Every feature must serve this mission directly. Resist the urge to add features for completeness — only build what delivers clear practical value, find the optimal balance between quality and cost, and prefer depth over breadth.

### Implementation guidelines

- Extend `AgentConfig` fields and generic adapter logic instead of adding per-CLI code paths.
- Config migrations are implicit — backfill defaults at load time, never require user config edits.
- `MergeEnv` in runner/ replaces (not appends) same-name env vars. This is critical for per-agent proxy configs overriding shell defaults.
- Stdout is machine-friendly (text or JSON). Stderr is for operator diagnostics.
- File injection must restore original content on exit. If the file didn't exist before, delete it.
- Minimize CLI flag proliferation — prefer YAML config with sensible defaults over new flags. Flags are for per-invocation overrides, not routine configuration.
