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

QuanCode is a Go CLI that launches a primary AI coding agent and lets it delegate tasks to other CLIs as sub-agents. All CLIs use the same data-driven `genericAgent` adapter — no per-CLI Go code needed.

### Package flow

```
cmd/start.go → prompt/injection.go → agent/agent.go (LaunchAsPrimary)
cmd/delegate.go → router/router.go → agent/agent.go (Delegate) → runner/
                                                                → ledger/
```

### Key packages

- **agent/** — Single `genericAgent` struct implements the `Agent` interface for any CLI. Behavior is driven by config fields (`PromptMode`, `TaskMode`, `OutputMode`, `DelegateArgs`, `OutputFlag`, `Env`). Adding a new CLI means adding config, not Go code.
- **config/** — YAML config with search order: `--config` flag (must exist) > `./quancode.yaml` > `~/.config/quancode/quancode.yaml` > built-in defaults. `applyKnownAgentDefaults()` backfills newer fields into older config files for backward compatibility.
- **prompt/** — Builds the system prompt injected into the primary CLI. Uses `text/template`. Excludes the actual primary from the listed agents.
- **router/** — `SelectAgent()` picks the best sub-agent: preferred_for keyword match > priority number > alphabetical.
- **runner/** — Process execution with timeout, stdin piping, output file capture, env merging (`MergeEnv` replaces same-name keys, not appends). Also handles git worktree isolation and patch collection.
- **ledger/** — JSONL logs at `~/.config/quancode/logs/{date}.jsonl`. Quota system supports calls/minutes/hours units with monthly/weekly/rolling_hours reset modes.

### Prompt injection modes

The primary CLI receives delegation instructions via one of:
- `append_arg` — system prompt as final CLI argument (Claude Code: `--append-system-prompt`)
- `file` — inject between `<!-- quancode:begin/end -->` markers in a file (Codex: `AGENTS.md`). Original content is restored on exit via a closure returned by `injectPromptFile`. Uses `runManagedPrimary` (child process with signal forwarding) instead of `syscall.Exec` so the defer runs.
- `env` / `stdin` — via environment variable or stdin pipe

### Delegation isolation modes

`--isolation inplace` (default): run in working directory, detect changes via git status snapshot diff.
`--isolation worktree`: git worktree, collect patch, auto-apply to main directory.
`--isolation patch`: like worktree but returns patch without applying.

## Design principles

- Extend `AgentConfig` fields and generic adapter logic instead of adding per-CLI code paths.
- Config migrations are implicit — backfill defaults at load time, never require user config edits.
- `MergeEnv` in runner/ replaces (not appends) same-name env vars. This is critical for per-agent proxy configs overriding shell defaults.
- Stdout is machine-friendly (text or JSON). Stderr is for operator diagnostics.
- File injection must restore original content on exit. If the file didn't exist before, delete it.
