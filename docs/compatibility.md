# Compatibility

QuanCode is an orchestration layer around third-party coding CLIs. Compatibility depends on both QuanCode behavior and the behavior of each upstream CLI.

## Supported Agents

QuanCode ships built-in defaults for:

- Claude Code
- Codex CLI
- Qoder CLI
- Gemini CLI
- GitHub Copilot CLI

"Supported" means the agent has a built-in adapter in `config/defaults.go`, its command can be detected in `PATH`, and core delegation flows have been smoke tested with recent CLI versions.

Additional CLIs (Amp, Goose, Cline CLI, Kiro CLI, aichat, OpenCode) have built-in adapter defaults but have not been validated. They may work but are not listed as supported.

## Compatibility Matrix

| CLI | Built-in adapter | Primary start | Delegate | Isolation modes | Notes |
|---|---|---|---|---|---|
| Claude Code | Yes | Smoke tested | Smoke tested | inplace, worktree, patch | Default primary; timeout 480s |
| Codex CLI | Yes | Smoke tested | Smoke tested | inplace, worktree, patch | File-based prompt injection via AGENTS.md; timeout 480s |
| Qoder CLI | Yes | Not yet validated | Smoke tested | inplace only | worktree/patch mode: files not written to disk |
| Gemini CLI | Yes | Smoke tested | Smoke tested | inplace, worktree, patch | File-based prompt injection via GEMINI.md; timeout 420s |
| Copilot CLI | Yes | Not yet validated | Smoke tested | inplace, worktree, patch | `--yolo` and `--no-auto-update` for delegation |

### Unvalidated Agents

These agents have built-in adapter configs in `config/defaults.go` and are auto-detected, but have not been smoke tested:

| CLI | Command | Notes |
|---|---|---|
| Amp | `amp` | Sourcegraph coding agent |
| Goose | `goose` | Block's autonomous agent |
| Cline CLI | `cline` | Plan-and-act workflow |
| Kiro CLI | `kiro-cli` | AWS spec-driven development |
| aichat | `aichat` | Multi-provider chat with RAG |
| OpenCode | `opencode` | Multi-model exploration |

### Host Environment

| Dimension | Status |
|---|---|
| Minimum Go | 1.25+ (from go.mod) |
| CI-tested OS | Linux (ubuntu-latest), macOS (macos-latest) |

## Feature Support Matrix

| Feature | Status | Since | Notes |
|---|---|---|---|
| Sync delegation | Stable | v0.1.0 | Core delegation with fallback |
| Isolation modes (inplace/worktree/patch) | Stable | v0.3.0 | Per-agent `supported_isolations` since v0.6.2 |
| Context injection (CLAUDE.md, AGENTS.md) | Stable | v0.4.0 | `--context-files`, `--context-diff`, `--no-context` |
| Post-delegation verification | Stable | v0.4.0 | `--verify`, `--verify-strict` |
| Async delegation | Stable | v0.5.0 | `--async` with job lifecycle management |
| Speculative parallelism | Stable | v0.6.0 | Parallel-collect strategy since v0.8.10 |
| Pipeline (multi-stage) | Stable | v0.7.0 | Per-stage fallback since v0.8.0 |
| Web dashboard | Preview | v0.8.3 | `quancode dashboard`, read-only, local only |

## What Is Not Guaranteed

- Version-by-version compatibility matrix
- Identical behavior across macOS and Linux for every CLI
- Zero prompt-injection differences between CLIs
- Production-stable guarantees for every isolation path

## Known Behavioral Differences

- Different CLIs use different prompt injection modes (CLI args, env, managed file)
- `prompt_mode=stdin` is not supported for primary interactive launch
- File-based prompt injection restores original content on primary exit
- Delegation output depends on the upstream CLI's output conventions
- Qoder CLI does not support worktree/patch isolation — auto-downgraded to inplace with a warning (since v0.6.1)
- Speculative execution skips backup agents whose isolation is incompatible (since v0.6.1)
- Async delegation does not support `--verify` (planned for future release)

## Reporting Issues

When filing a compatibility issue, include: CLI name, CLI version, OS, `quancode version`, and exact reproduction steps.
