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
cmd/pipeline.go → config/pipeline.go (LoadPipeline) → cmd/delegate_attempt.go (per stage)  → runner/
                                                                                             → ledger/
cmd/delegate_async.go → job/ (write pending) → cmd/run_job.go (detached) → delegate_attempt.go
cmd/job*.go → job/ (list/status/result/logs/cancel/clean)
cmd/batch.go → batch/ (manifest+state) → cmd/delegate_attempt.go (per item)   → runner/
                                                                                → ledger/
cmd/dashboard.go → server/ (HTTP server) → ledger/ + job/ + active/ (read-only)
                                          → web/ (embedded frontend via go:embed)
```

### Key packages

- **agent/** — Single `genericAgent` struct implements the `Agent` interface for any CLI. Behavior is driven by config fields (`PromptMode`, `TaskMode`, `OutputMode`, `DelegateArgs`, `OutputFlag`, `Env`). Adding a new CLI means adding config, not Go code.
- **config/** — YAML config with search order: `--config` flag (must exist) > `./quancode.yaml` > `~/.config/quancode/quancode.yaml` > built-in defaults. `applyKnownAgentDefaults()` backfills newer fields into older config files for backward compatibility.
- **context/** — Builds delegation context bundles by auto-injecting project instruction files such as `CLAUDE.md` and `AGENTS.md`, with support for explicit files, git diff injection, size budgets, and path safety checks.
- **prompt/** — Builds the system prompt injected into the primary CLI. Uses `text/template`. Excludes the actual primary from the listed agents.
- **router/** — `SelectAgent()` picks the best sub-agent: preferred_for keyword match > priority number > alphabetical.
- **runner/** — Process execution with timeout, stdin piping, output file capture, env merging (`MergeEnv` replaces same-name keys, not appends). Also handles git worktree isolation and patch collection. All processes run in their own process group (`Setpgid`); timeout kills the entire group to prevent child process leaks. `RunWithContext` variants accept external contexts for speculative cancellation.
- **ledger/** — JSONL logs at `~/.config/quancode/logs/{date}.jsonl`. Records each delegation with agent, task, duration, exit code, changed files, and fallback chain. Also provides ID generation (NewDelegationID, NewRunID, NewPipelineID) for tracking. Pipeline entries include PipelineID, PipelineName, StageName, StageIndex for workflow-level grouping. Delegation output is stored in `logs/outputs/{delegationID}.output` via `WriteOutput`.
- **active/** — Lightweight file-based registry at `~/.config/quancode/active/` tracking currently running synchronous delegations. Each running task writes a marker file with PID and start time; `List()` scans the directory and cleans up stale entries via PID liveness checks. Only sync delegations register here; async jobs already have persistent state in `job/`.
- **server/** — HTTP server for the web dashboard. Provides REST API endpoints (`/api/delegations`, `/api/delegations/{id}/output`, `/api/jobs`, `/api/agents`, `/api/stats`, `/api/events`, `/api/version`) with pagination, filtering, SSE broadcast, and stats caching. Uses Go 1.22 enhanced `ServeMux` routing. Graceful shutdown on SIGTERM/SIGINT.
- **web/** — Embedded frontend assets. Single-file `index.html` with Alpine.js and Tailwind CSS (vendored in `static/`). Exported via `go:embed` as `web.Assets`. In `--dev` mode, files are served from the filesystem for live editing.
- **health/** — Agent circuit breaker, **derived from the ledger rather than stored**. `NewSnapshot` reads recent entries and opens a breaker for any agent with N consecutive *agent-fault* failures, with exponential backoff. Only quota/upstream/auth failures count — timeouts and task failures never do, so a hard task cannot disable a healthy agent. Never blocks an explicitly named agent; fails open by force-probing exactly one candidate when all are unhealthy.
- **batch/** — Frozen manifest plus mutable per-item state for `quancode batch`, at `~/.config/quancode/batches/{id}.json`. The manifest (template text, item list, workdir, agent) is immutable so resume cannot silently pick up an edited template; the ledger cannot replace it because it only records what ran, never what *should* run. Atomic writes under flock.
- **job/** — Persistent async job state at `~/.config/quancode/jobs/`. Atomic writes via flock+CAS with schema versioning. Handles job lifecycle (pending→running→succeeded/failed/timed_out/cancelled/lost), TTL cleanup, PID reuse detection via `pid_start_time`, and capped output files (50MB).

### Dashboard (preview)

`quancode dashboard` starts a local HTTP server providing a web UI for monitoring all delegation activity. The `server/` package implements REST API handlers backed by `ledger/`, `job/`, and `active/` data. The `web/` package embeds a single-file HTML frontend (Alpine.js + Tailwind CSS, vendored for offline use) via `go:embed`. The server listens on `127.0.0.1` only, serves read-only APIs, and supports SSE for real-time updates. Active sync tasks are tracked via file markers in `active/` and shown alongside async job counts.

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

### Pipeline (multi-stage delegation)

`quancode pipeline <name-or-file> [task]` runs an ordered sequence of delegation stages defined in YAML. Creates a pipeline-level worktree where all stages execute as `inplace`, with code changes accumulating naturally. Stage outputs flow to subsequent stages via Go `text/template` variables (`{{.Input}}`, `{{.Prev.Output}}`, `{{.Stages.NAME.Output}}`). Pipeline definitions are loaded from explicit paths, `.quancode/pipelines/`, or `~/.config/quancode/pipelines/`. Each stage can specify its own agent, timeout, verify commands, and on_failure policy (`stop`/`continue`). Final patch is collected via `CollectPatchSince(baseSHA)` to capture both committed and uncommitted changes.

### Agent health circuit breaker

Automatic routing (initial selection, fallback, speculative backup, `route` preview) skips agents that are currently broken. Health is derived from recent ledger entries — no second source of truth, no read-modify-write races between concurrent delegations. Only `agent_fault` failures count toward the breaker; timeouts track task difficulty, not agent health. Configure via `preferences.agent_health`; absent config means enabled.

Failure patterns live in one table (`config.CommonFailurePatterns`) carrying both `Transient` (drives fallback) and `AgentFault` (drives the breaker), so the fallback decision and the user-facing diagnostic hint cannot drift apart. Add new patterns by pasting the CLI's real message verbatim.

### Batch delegation

`quancode batch` applies one task template across many items, one delegation per item, serially. Successful items are never re-run, so an interrupted or partly failed batch resumes without redoing work. Resume distinguishes transient failures (retried automatically) from deterministic ones (skipped unless `--retry-failed`), so a task that fails the same way every time stops burning quota. `--dry-run` validates every item renders and shows the first and last prompt before anything executes.

Execution is serial by design: batch is the heaviest thing QuanCode does, and parallel items multiply the rate at which a shared account hits its quota.

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
