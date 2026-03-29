# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning in spirit, with alpha releases allowed to change behavior more quickly while the public interface settles.

## [Unreleased]

## [v0.4.12] - 2026-03-29

### Fixed

- Delegate 前自动清理孤儿 worktree，防止 SIGKILL 后磁盘泄漏

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
