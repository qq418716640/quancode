# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning in spirit, with alpha releases allowed to change behavior more quickly while the public interface settles.

## [Unreleased]

## [v0.4.14] - 2026-03-29

### Added

- Prompt 新增 TASK TYPES 指引，区分代码修改/研究分析/代码审查任务
- KnownAgents 新增 Gemini CLI、Copilot CLI、Amp、Goose、Cline CLI、Kiro CLI、aichat 自动检测
- 并行委派简化：patch 自动缓存 + `apply-patch --id` 按 delegation_id 直接应用
- `QUANCODE_DEBUG=1` 调试模式，输出自动更新等后台操作的诊断信息

### Fixed

- stderr 消息统一英文
- OpenCode 的 DelegateArgs 修正为 `["-p"]`
- `init` 支持列表改为从 KnownAgents 动态生成

### Removed

- 文档中移除未经验证的 Aider 和 OpenCode 引用（代码层保留自动检测）

## [v0.4.13] - 2026-03-29

### Added

- 后台静默自动更新：每 2 小时检测 GitHub 最新版本，有新版本自动下载替换
- 支持 brew 和直接二进制两种更新路径
- `QUANCODE_SKIP_UPDATE_CHECK=1` 环境变量可禁用

## [v0.4.12] - 2026-03-29

### Fixed

- Delegate 前自动清理孤儿 worktree，防止 SIGKILL 后磁盘泄漏
- 孤儿清理跳过 1 小时内创建的目录，避免并发委派时误删

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
