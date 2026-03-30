# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning in spirit, with alpha releases allowed to change behavior more quickly while the public interface settles.

## [Unreleased]

### Added

- **Async delegation**: `delegate --async` runs tasks in a detached background process, freeing the primary agent to continue working
  - Requires `--isolation worktree` or `--isolation patch` (inplace not allowed)
  - `--timeout` flag to set per-task timeout (default: agent config `timeout_secs`)
  - Returns a `job_id` immediately; background runner handles execution, fallback, and result collection
- **Job management commands**: `quancode job list|status|result|logs|cancel|clean`
  - `job list` with `--workdir`, `--limit`, `--latest`, `--format json` filtering
  - `job status` and `job result` with JSON output support
  - `job logs` with `--tail` for viewing agent output
  - `job cancel` with SIGTERM→SIGKILL and idempotent handling
  - `job clean --ttl` for removing expired job files and orphan artifacts
- New `job/` package: persistent job state with flock+CAS atomic writes, schema versioning, TTL cleanup, PID reuse detection via `pid_start_time`
- `AgentConfig.NonInteractiveArgs` field for async-mode-specific agent arguments
- Async delegation guidance in system prompt injection template

### Changed

- `runDelegateAttempt` refactored to accept `DelegateAttemptOptions` struct with `Quiet` mode for non-interactive execution
- `runVerification` / `runSingleVerify` no longer produce stderr output; logging handled by `runAndLogVerification` wrapper
- Isolation resolution standardized: empty string normalized to `"inplace"` after config resolution

### Known Limitations

- `delegate --async` does not support `--verify`/`--verify-strict` (planned for future release)
- `delegate --async` does not pass `--context-files`/`--context-diff` flags (uses default context only)

## [v0.4.18] - 2026-03-29

### Fixed

- Auto-update no longer downgrades when local version is newer than latest release — added semver comparison (`isNewer`) to replace naive string inequality check

## [v0.4.17] - 2026-03-29

### Added

- Delegation ceremony: rich stderr output with start/success/failure messages, elapsed time, and changed file count
- Spinner animation during delegation (TTY-aware, degrades to static line in non-TTY)
- Fallback chain visualization: `Chain: claude (timed_out) → codex ✓`
- New `ui/` package for terminal output utilities

### Changed

- Extracted `FormatDuration` to `ui` package, shared by `cmd/stats` and ceremony output

## [v0.4.16] - 2026-03-29

### Fixed

- Statusline percentage display: round floats to avoid precision noise (e.g. `14.000000000000002%` → `14%`)

## [v0.4.15] - 2026-03-29

### Removed

- Removed quota system — limits were arbitrary and unenforceable; ledger and stats remain for auditing
- Removed approval system — no sub-agent implements the file-based approval protocol; sub-agents use their own permission systems or run in full-auto mode
- Moved ID generation helpers (NewDelegationID, NewRunID) from approval package to ledger/ids.go

## [v0.4.14] - 2026-03-29

### Added

- Prompt TASK TYPES guidance: differentiate code modification, research/analysis, and code review tasks
- KnownAgents: added Gemini CLI, Copilot CLI, Amp, Goose, Cline CLI, Kiro CLI, aichat auto-detection
- Parallel delegation: patch auto-caching + `apply-patch --id` to apply by delegation ID
- `QUANCODE_DEBUG=1` debug mode for background operation diagnostics

### Fixed

- Unified all stderr messages to English
- OpenCode DelegateArgs corrected to `["-p"]`
- `init` supported commands list now generated dynamically from KnownAgents

### Removed

- Removed unvalidated Aider and OpenCode references from docs (code-level auto-detection retained)

## [v0.4.13] - 2026-03-29

### Added

- Background silent auto-update: checks GitHub latest release every 2 hours, downloads and replaces binary
- Supports both brew and direct binary update paths
- `QUANCODE_SKIP_UPDATE_CHECK=1` environment variable to disable

## [v0.4.12] - 2026-03-29

### Fixed

- Auto-prune orphan worktrees before each delegation to prevent disk leaks from SIGKILL
- Orphan cleanup skips directories less than 1 hour old to avoid concurrent delegation conflicts

## [v0.4.11] - 2026-03-29

### Added

- User preferences block in config: `default_isolation` and `fallback_mode`
- Enum validation for `prompt_mode`, `task_mode`, `output_mode` in config

## [v0.4.0] - 2026-03-29

### Theme: Delegation Observability & Resilience

Completes the delegation execution loop — from context injection and verification, through failure classification and fallback chain tracking, to patch conflict recovery and dry-run preview.

### Added

- Automatic project context injection for delegations (`CLAUDE.md`, `AGENTS.md`)
- `--context-files`, `--context-diff`, `--context-max-size`, and `--no-context` flags
- Post-delegation verification with `--verify` and `--verify-strict`
- `--verify-timeout` flag for verification command timeout
- `context_defaults` and per-agent `context` configuration in YAML config
- `FinalStatus` and verification results in ledger entries
- Ledger run/attempt tracking: `RunID`, `Attempt`, `FallbackFrom`, `FallbackReason` fields link multiple attempts within a single delegate invocation
- `quancode stats` fallback chain analysis: trigger rate, recovery rate, reason distribution, agent chain frequency
- `delegate --dry-run` preview mode: shows the full prompt (context + task) without executing, supports text/json output
- Patch apply failure recovery: conflict pre-check via `git apply --check` prevents work tree pollution, outputs preserved patch and conflict file list for manual recovery
- Unified failure classification (`FailureClass`): `launch_failure`, `timed_out`, `rate_limited`, `agent_failed`, `patch_conflict`, `verify_failed`
- `quancode stats` failure breakdown section
- Core design philosophy and flag restraint principle in CLAUDE.md

### Changed

- `buildDelegationResult` now accepts an `attemptResult` struct instead of 11 parameters
- `applyPatch` failure in `worktree` mode is now an error instead of a warning
- Fallback rebuilding now regenerates the context bundle per agent so agent-specific `context` config is respected
- Fallback logic now driven by `FailureClass` via `isTransientFailure()` instead of direct stderr pattern matching
- `FallbackReason` in ledger entries now uses `FailureClass` values instead of separate constants
- `DelegationResult` JSON output includes `conflict_files` and `patch` on apply failure

## [v0.3.0] - 2026-03-28

### Theme: Parallel Delegation & Worktree Hardening

### Added

- `quancode apply-patch` command for manual patch application with preview
- Parallel delegation support via prompt guidance + `--isolation patch` + `apply-patch`
- Worktree auto-exclusion of build caches (`.tmp/`, `.gocache/`, `node_modules/`, etc.)
- `PatchSummary` function for patch preview before applying

### Fixed

- Worktree patches no longer collect build cache artifacts
- `.tmp/` added to `.gitignore` to prevent worktree cache pollution

## [v0.2.0] - 2026-03-28

### Theme: Skill Integration & CLI Ecosystem

### Added

- `/quancode` skill for Claude Desktop, Cowork, and Dispatch multi-agent delegation
- `quancode skill install/uninstall` commands
- Skill explicitly blocks `quancode start` usage, enforcing delegate-only mode

## [v0.1.0] - 2026-03-27

Rapid iteration from first alpha to feature-complete core (v0.1.0 through v0.1.9).

### Added

- Interactive approval system (no separate terminal needed)
- Agent auto-fallback on timeout or rate-limit
- Multi-rule quota system per agent
- Session identity (terminal title, env var, startup banner)
- Flag value auto-completion (`--primary`, `--agent`, `--format`, `--isolation`)
- `quancode init` with Claude Code statusline and auto-detection of installed agents
- Built-in Qoder CLI adapter configuration

### Fixed

- Fallback implementation issues found during codex+qoder review
- Statusline cost display formatted to two decimal places

## [v0.1.0-alpha] - 2026-03-27

First public alpha. Primary-agent startup, delegation flows, file-based prompt injection, config backfills, CI, and initial test coverage.
