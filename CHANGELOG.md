# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning in spirit, with alpha releases allowed to change behavior more quickly while the public interface settles.

## [v0.8.15] - 2026-04-05

### Added

- **Dashboard auto-start**: new `dashboard_mode` preference (`auto`/`off`) enables the web dashboard to launch automatically as a background process when running `quancode start`
  - `quancode dashboard enable` — enable auto-start and launch immediately
  - `quancode dashboard disable` — disable auto-start and suppress tips
  - `quancode dashboard status` — show current preference and running state
  - Health check via `/api/version` distinguishes QuanCode from other services on the same port
  - Respects `--config` flag for config writes; validates `dashboard_port` range (1-65535)
  - Interactive terminals see a one-time tip when dashboard_mode is unconfigured

## [v0.8.14] - 2026-04-05

### Improved

- **Dashboard manual refresh button**: refresh data without losing current tab selection or active filters

## [v0.8.13] - 2026-04-05

### Added

- **Global minimum delegation timeout** (`min_timeout_secs`): new preference that sets a floor on effective delegation timeout — prevents tasks from being killed prematurely when `--timeout` or agent `timeout_secs` is set too low. Applies to all execution paths: sync, async, speculative, and pipeline

### Improved

- **Prompt: explicit inplace for read-only tasks**: delegation instructions now tell the primary agent to always pass `--isolation inplace` for read-only delegations (code review, research, analysis), preventing unnecessary worktree creation and speculative execution when the user has configured `default_isolation: worktree`
- **Single warning site for timeout floor**: timeout-raised warnings are emitted only once per delegation path, eliminating duplicate stderr messages in speculative and async modes

## [v0.8.12] - 2026-04-05

### Improved

- **Dashboard stats follow filters**: stats cards now dynamically update when changing agent, status, or time range filters — backend `/api/stats` accepts filter params, filtered requests bypass cache
- **Dashboard stats follow tabs**: switching between Delegations, Async Jobs, and Pipelines tabs shows contextual statistics (total, success rate, avg duration, active count) computed from each tab's data

## [v0.8.11] - 2026-04-04

### Fixed

- **Context-diff + worktree isolation consistency**: when `--context-diff working/staged` is used with worktree isolation, the diff is now applied to the worktree before agent execution so the agent sees files consistent with the prompt. Patch collection uses the post-apply baseline to isolate only agent changes
- CI workflow now configures git identity for tests that require commits

## [v0.8.10] - 2026-04-04

### Changed

- **Speculative execution: parallel-collect strategy** — both agents now run to completion instead of cancelling the slower one. Primary result is preferred when successful; companion result is preserved in JSON output (`speculative.companion` field) and ledger for downstream synthesis
  - New `selected` / `selection_reason` ledger fields replace deprecated `cancelled_by`
  - JSON output includes `SpeculativeInfo` with companion agent's full result
  - Text mode stdout unchanged (selected output only); companion availability noted on stderr

### Improved

- **Dashboard pipeline view**: stages with fallback retries are now grouped under one node instead of appearing as separate stages; click any stage to expand Task/Output detail panel
- **Dashboard copy buttons**: all copy buttons now show "Copied!" feedback for 1.5s; added `execCommand` fallback for non-HTTPS contexts
- **Dashboard delegation status**: uses `final_status` for display — cancelled shows as grey CANCEL, timed\_out as yellow TIMEOUT instead of red FAIL

## [v0.8.9] - 2026-04-04

### Fixed

- Dashboard "View Output" now requests up to 10,000 lines instead of the default 500, fixing truncated output display for large agent responses

## [v0.8.8] - 2026-04-04

### Added

- **Sync delegation output recording**: agent output from synchronous delegations is now persisted to `~/.config/quancode/logs/outputs/` and viewable in the Dashboard via "View Output" button
  - New `GET /api/delegations/{id}/output` API endpoint with delegation ID format validation and tail support
  - Ledger entries now include `delegation_id` and `output_file` fields for traceability
  - Covers all delegation paths: normal, fallback, speculative, and pipeline stages

### Changed

- **Dashboard UI refresh**: sticky header/tabs, skeleton loading states, rounded pill badges, chevron expand indicators, improved typography and spacing
- Dashboard header now displays the QuanCode version
- Extracted shared `serveOutputFile` helper — deduplicated output tail logic between delegation and job output handlers

## [v0.8.7] - 2026-04-04

### Fixed

- Agent filter dropdown in Dashboard now reads available agents from config instead of ledger data, ensuring disabled or never-used agents don't appear

## [v0.8.6] - 2026-04-04

### Fixed

- **Dashboard "Active Tasks" now tracks running sync delegations**: new `active/` package writes marker files during synchronous delegation execution; Dashboard counts both active sync tasks and non-terminal async jobs in real time
- Renamed card label from "Active Jobs" to "Active Tasks" to reflect combined sync + async scope

## [v0.8.5] - 2026-04-04

### Fixed

- Dashboard delegation row expand now works inline (detail panel appears in the clicked row instead of at the bottom of the table)

### Changed

- **Copilot agent config**: updated description, added `multi-model` strength, `--yolo` and `--no-auto-update` to `DelegateArgs`, added `PrimaryArgs: ["--yolo"]`

## [v0.8.4] - 2026-04-04

### Fixed

- Dashboard delegation row expand now uses stable IDs instead of array index, preventing wrong row from expanding after data refresh
- Updated Copilot agent config: added `multi-model` strength, `--yolo` and `--no-auto-update` to `DelegateArgs`

## [v0.8.3] - 2026-04-04

### Added

- **Web dashboard (preview)**: `quancode dashboard` starts a local HTTP server with a browser-based UI for monitoring all delegation activity
  - REST API: `/api/delegations` (paginated, filterable), `/api/jobs`, `/api/jobs/{id}`, `/api/jobs/{id}/output` (tail support), `/api/stats` (cached)
  - Real-time updates via Server-Sent Events (`/api/events`) with broadcast mode and delta-only push
  - Single-file frontend (Alpine.js + Tailwind CSS, vendored for offline use) with dark theme
  - Three views: delegation history table, async jobs panel, pipeline stage visualization
  - Flags: `--port` (default 8377), `--dev` (serve from filesystem), `--open` (auto-open browser)
  - Listens on `127.0.0.1` only, read-only APIs, no authentication required

## [v0.8.2] - 2026-04-04

### Fixed

- **Async job cancellation now terminates subprocesses**: SIGTERM/SIGINT from `job cancel` propagates through a shared `parentCtx` to kill running agent processes via context cancellation, instead of leaving orphan processes
- **Fallback isolation filtering**: `fallbackLoop.nextAgent()` now skips agents that don't support the required isolation mode — fixes delegate, async, and pipeline paths where an inplace-only agent (e.g. Qoder) could be selected as fallback for worktree/patch jobs
- **Async fallback inherits NonInteractiveArgs and effective timeout**: fallback agents in async jobs now get the same preparation as the primary (timeout override + non-interactive flags), preventing interactive hangs in detached processes
- **Speculative+patch verification**: skip post-race verification in patch isolation mode since the winner's patch is not applied to the working directory — previously verified the unmodified baseline
- **`runManagedPrimary` process group management**: file-mode primary launch now uses `Setpgid` and sends signals to the entire process group, ensuring grandchild processes are terminated on Ctrl-C/SIGTERM
- **`DelegateWithContext` safety timeout**: applies agent's own timeout when caller's context has no deadline, preventing infinite execution
- **`killProcessGroup` fallback**: group kill failure now falls back to single-process kill instead of returning the error
- **Slice mutation in async delegation**: `DelegateArgs` append uses a fresh slice copy to avoid corrupting the shared config's underlying array across fallback iterations
- **Signal vs completion semantics**: async job runner uses a dedicated `signalled` channel (only closed on SIGTERM/SIGINT) instead of overloaded `cancelled` channel, preventing fallback from being skipped after normal agent completion

## [v0.8.1] - 2026-04-03

### Fixed

- `logPipelineEntry` now correctly records fallback attempt number and `fallback_from`/`fallback_reason` instead of hardcoded `Attempt: 1`
- `nextAgent` returns routing reason — fallback log messages restored to `falling back to X (reason)` format
- Fix double chain append in delegate `no fallback available` branch (inherited from pre-v0.8.0)
- Pipeline fallback test now verifies fallback actually triggers (rate-limited agent → ok-agent)

## [v0.8.0] - 2026-04-03

### Added

- **Per-stage fallback in pipelines**: pipeline stages now automatically retry with a different agent on transient failures (timeout, rate limit, launch failure), matching the existing delegation fallback behavior
- Git checkpoint/restore for pipeline fallback: before each stage, a checkpoint commit is created; on fallback, the worktree is restored to prevent dirty state from affecting the retry agent
- `validateTemplateRefs`: pipeline template references (e.g., `{{.Stages.X.Output}}`) are validated at parse time — forward references and nonexistent stage names are caught before execution
- `fallbackLoop` helper: extracted reusable agent selection and retry logic from the delegation fallback loop, now shared by both `delegate` and `pipeline` commands
- Per-stage fallback chain recorded in ledger: each retry attempt gets its own ledger entry with `fallback_from` and `fallback_reason` for full traceability

### Changed

- Refactored `delegate` command fallback loop to use the shared `fallbackLoop` helper, reducing duplication

## [v0.7.5] - 2026-04-03

### Fixed

- Worktree leak: delegation in `worktree`/`patch` isolation mode never cleaned up the git worktree after completion — `cleanupWorktree` was set to nil before the deferred cleanup could run

## [v0.7.4] - 2026-04-03

### Added

- `AgentConfig.FallbackIsolation()` method — centralizes isolation fallback logic (DefaultIsolation → SupportedIsolations[0] → inplace), eliminates duplication between delegate and speculative paths

### Changed

- Prompt: isolation mode guidance now explicitly directs primary agent to omit `--isolation` for read-only tasks (code review, research, analysis) to avoid unnecessary worktree overhead and agent exclusion from speculative execution
- Prompt: code review task type now explicitly warns against `--isolation worktree`
- Speculative execution comment clarifies why mixed isolation modes are rejected (semantic mismatch between worktree apply vs patch-only)

### Fixed

- Remove duplicate `v` prefix in session active version display (`vv0.7.3` → `v0.7.3`)

## [v0.7.3] - 2026-04-03

### Added

- `applyKnownAgentDefaults` now backfills all zero-value fields (Name, Description, Strengths, PrimaryArgs, DelegateArgs, PreferredFor, Priority, etc.) — user configs only need to specify overrides
- Full auto-approve flags added to all agent defaults: Claude (`--dangerously-skip-permissions`), Codex (`-s danger-full-access`), Qoder (`--dangerously-skip-permissions`), Gemini (`--yolo`)

## [v0.7.2] - 2026-04-03

### Added

- Complete Gemini CLI agent configuration: PromptMode file injection via GEMINI.md, non-interactive delegation args (`--yolo -o text`), refined routing keywords and timeout

### Fixed

- Reorder Gemini DelegateArgs so `-p` is last before task text — Gemini CLI requires prompt value immediately after `-p`

## [v0.7.1] - 2026-04-03

### Added

- Prompt injection now includes pipeline guidance — primary agent can autonomously decide when to use `quancode pipeline` vs single `delegate` for multi-phase tasks

## [v0.7.0] - 2026-04-02

### Added

- **Pipeline (multi-stage delegation)**: `quancode pipeline <name-or-file> [task]` runs an ordered sequence of delegation stages defined in YAML, with inter-stage output passing via Go template variables (`{{.Input}}`, `{{.Prev.Output}}`, `{{.Stages.NAME.Output}}`)
- Pipeline definitions loaded from explicit paths, `.quancode/pipelines/`, or `~/.config/quancode/pipelines/`
- Per-stage agent override, timeout, verify commands, and `on_failure` policy (`stop`/`continue`)
- Pipeline-level worktree isolation: all stages execute as inplace within a shared worktree, changes accumulate naturally
- `CollectPatchSince(baseSHA)` captures both committed and uncommitted changes across pipeline stages
- Pipeline-level verification commands (`verify`/`verify_strict` in pipeline YAML)
- Ledger entries now include `pipeline_id`, `pipeline_name`, `stage_name`, `stage_index` for workflow-level grouping
- `--dry-run` mode shows execution plan without running
- JSON and text output formats for pipeline results
- Show version number in startup banner (`quancode start`)

## [v0.6.3] - 2026-04-02

### Fixed

- Async delegation ledger entries now use `completed` instead of `succeeded` for final_status, consistent with sync delegation path
- Fixed flaky `TestRunSuccess` duration assertion on fast CI (>0 → >=0)

### Added

- Comprehensive edge case tests for config, router, cmd, and runner modules

## [v0.6.2] - 2026-04-02

### Added

- `supported_isolations` capability field on AgentConfig — agents can declare which isolation modes they support; incompatible modes are auto-downgraded with a warning
- Config validation now catches `default_isolation` not in `supported_isolations`
- Context size warning when total prompt exceeds 24KB
- Claude and Codex default timeout increased from 300s to 480s (other agents remain at 300s)
- Prompt: code review guidance for large diffs (>300 lines split by module), async delegation hints, Bash timeout bumped to 480000ms

## [v0.6.1] - 2026-04-02

### Added

- Per-agent `default_isolation` config field — allows agents incompatible with certain isolation modes (e.g. Qoder + worktree) to override the global default
- Speculative parallelism now skips backup agents whose per-agent isolation is incompatible with the current isolation mode
- Qoder built-in default set to `inplace` isolation (worktree incompatible due to upstream cwd behavior)

## [v0.6.0] - 2026-04-02

### Added

- **Speculative parallelism**: when `preferences.speculative_delay_secs > 0` and isolation is worktree/patch, a backup agent launches in parallel after the delay window — first success wins, loser is cancelled
- **Process group management**: all subprocess execution uses Setpgid + group kill to prevent child process leaks on timeout
- `RunWithContext`, `RunWithStdinContext`, `RunWithOutputFileContext` runner variants for external cancellation
- `Agent.DelegateWithContext` interface method for context-controlled delegation
- `preferences.speculative_delay_secs` config field (default 0 = disabled)
- Ledger fields: `speculative`, `speculative_role`, `cancelled_by` for tracking speculative execution
- `speculative_cancelled` failure class
- Ledger entries now include `version` field recording the quancode version that produced the entry

## [v0.5.4] - 2026-04-01

### Added

- Ledger entries now include `version` field recording the quancode version that produced the entry

## [v0.5.3] - 2026-04-01

### Added

- `--timeout` flag now works for sync delegation (previously async-only), capped at agent config `timeout_secs`
- New prompt sections: BEFORE DELEGATING (task sizing signals and split strategies) and TIMEOUT CONTROL
- Negative `--timeout` values are now rejected with a clear error

### Changed

- `DelegateOptions` gains `TimeoutOverride` field for per-task timeout control
- `--timeout` flag description updated from async-specific to general

## [v0.5.2] - 2026-03-31

### Fixed

- Increased Codex CLI default timeout from 180s to 300s for consistency with other agents
- Added Bash timeout guidance in delegation prompt (300000ms minimum) to prevent premature process kills

## [v0.5.1] - 2026-03-31

### Changed

- Delegation prompt no longer biased toward coding tasks — now covers research, documentation, writing, and analysis scenarios equally
  - Opening description changed from "AI coding agents" to "AI agents"
  - Added Documentation/writing task type with guidance on audience, structure, and tone
  - Added non-coding Good example (approach comparison with markdown table output)
  - Result checking now instructs primary agent to check exit_code/timed_out first, then task-appropriate deliverables (output, changed_files, or both)
  - Verification section clarifies non-code tasks do not need --verify
  - Isolation inplace mode clarifies suitability for read-only tasks
  - Broad/underspecified task warning promoted from research-only to universal rule

## [v0.5.0] - 2026-03-31

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

## [v0.3.2] - 2026-03-28

### Docs

- Added `apply-patch` and parallel delegation documentation, corrected Go version references

## [v0.3.1] - 2026-03-28

### Fixed

- Worktree auto-excludes build cache directories during patch collection
- `.tmp/` added to `.gitignore` to prevent worktree cache pollution

## [v0.3.0] - 2026-03-28

### Theme: Parallel Delegation & Worktree Hardening

### Added

- `quancode apply-patch` command for manual patch application with preview
- Parallel delegation support via prompt guidance + `--isolation patch` + `apply-patch`
- Worktree auto-exclusion of build caches (`.tmp/`, `.gocache/`, `node_modules/`, etc.)
- `PatchSummary` function for patch preview before applying

## [v0.2.1] - 2026-03-28

### Added

- `quancode skill install/uninstall` commands for Claude Desktop integration
- Skill explicitly blocks `quancode start` usage, enforcing delegate-only mode

### Docs

- Updated documentation to cover v0.2.0 new features

## [v0.2.0] - 2026-03-28

### Theme: Skill Integration & CLI Ecosystem

### Added

- `/quancode` skill for Claude Desktop, Cowork, and Dispatch multi-agent delegation

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
