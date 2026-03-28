# QuanCode Agent Guide

This repository builds `quancode`, a Go CLI that launches a primary coding agent and lets it delegate bounded tasks to other coding CLIs.

## Project Goal

- Keep primary-agent startup reliable for different CLIs.
- Keep delegation predictable, inspectable, and scriptable.
- Preserve working-directory safety when delegation or prompt injection touches repo files.

## Key Entry Points

- `main.go`: CLI entry.
- `cmd/root.go`: root Cobra command and global flags.
- `cmd/start.go`: starts the primary agent and injects delegation context.
- `cmd/delegate.go`: runs a sub-agent task and returns text or JSON.
- `prompt/injection.go`: builds the prompt content shown to the primary agent.
- `agent/agent.go`: data-driven adapter for primary launch and sub-agent execution.
- `config/config.go`: config loading and compatibility backfills.
- `config/defaults.go`: built-in agent definitions and defaults.
- `router/router.go`: agent auto-selection.
- `runner/`: process execution, isolated worktrees, patch application.
- `ledger/`: logging and quota tracking.

## Working Rules

- Prefer small, data-driven changes over CLI-specific branches.
- Preserve backward compatibility for existing `~/.config/quancode/quancode.yaml` files.
- Do not assume every agent supports the same prompt injection mode.
- Avoid persistent side effects in the user workspace unless they are restored before exit.
- Keep stderr useful for operator diagnostics; keep stdout machine-friendly where commands promise structured output.

## Primary Agent Behavior

- `quancode start` must work whether the primary is selected by config or by `--primary`.
- The injected prompt must list only enabled non-primary agents.
- If prompt injection uses a file such as `AGENTS.md`, treat it as a managed runtime artifact:
  restore original content on exit
  preserve file permissions
  fail loudly on read/write errors

## Delegation Behavior

- Use `quancode delegate` for sub-agent execution. Do not wire primary agents to call other CLIs directly.
- Keep delegated tasks single-shot and well-scoped.
- When changing delegation output, preserve both text mode and JSON mode semantics.
- Do not break `changed_files`, isolation modes, or ledger logging.

## Validation

- Run `go test ./...` after code changes.
- If behavior around startup or delegation changes, also manually exercise the affected CLI path.
- When touching prompt injection or file restoration, verify the target file is unchanged after the primary exits.

## Editing Guidance

- Follow existing Go style and keep comments sparse.
- Prefer extending `AgentConfig` and generic adapter logic instead of adding one-off code paths.
- Keep config migrations implicit where possible by backfilling defaults at load time.
- If a change affects operator-facing behavior, update stderr messaging to make failures diagnosable.
