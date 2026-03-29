# Compatibility

QuanCode is an orchestration layer around third-party coding CLIs. Compatibility depends on both QuanCode behavior and the behavior of each upstream CLI.

## Supported Agents

QuanCode ships built-in defaults for:

- Claude Code
- Codex CLI
- Qoder CLI

"Supported" means the agent has a built-in adapter in `config/defaults.go`, its command can be detected in `PATH`, and core delegation flows have been smoke tested with recent CLI versions.

Additional CLIs (Aider, OpenCode, etc.) have built-in adapter defaults but have not been validated. They may work but are not listed as supported.

## Compatibility Matrix

| CLI | Built-in adapter | Primary start | Delegate | Isolation modes | Notes |
|---|---|---|---|---|---|
| Claude Code | Yes | Smoke tested | Smoke tested | inplace, worktree, patch | |
| Codex CLI | Yes | Smoke tested | Smoke tested | inplace, worktree, patch | |
| Qoder CLI | Yes | Not yet validated | Smoke tested | inplace only | worktree/patch mode: files not written to disk |

### Host Environment

| Dimension | Status |
|---|---|
| Minimum Go | 1.25+ (from go.mod) |
| CI-tested OS | Linux (ubuntu-latest), macOS (macos-latest) |

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

## Reporting Issues

When filing a compatibility issue, include: CLI name, CLI version, OS, `quancode version`, and exact reproduction steps.
