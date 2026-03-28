# Compatibility

QuanCode is an orchestration layer around third-party coding CLIs. Compatibility depends on both QuanCode behavior and the behavior of each upstream CLI.

## Supported Agents

QuanCode ships built-in defaults for:

- Claude Code
- Codex CLI
- Qoder CLI
- Aider
- OpenCode

"Supported" means the agent has a built-in adapter in `config/defaults.go`, its command can be detected in `PATH`, and core delegation flows are expected to work with recent CLI versions. It does not mean every version is fully validated.

## Compatibility Matrix

Evidence levels:

- **built-in adapter**: QuanCode ships a default config for the CLI
- **manual smoke tested**: validated with explicit manual steps
- **not yet validated**: present in code but not covered by smoke guidance

| CLI | Built-in adapter | Primary start | Delegate | Notes |
|---|---|---|---|---|
| Claude Code | Yes | Expected to work | Expected to work | Smoke tested |
| Codex CLI | Yes | Expected to work | Expected to work | Smoke tested |
| Qoder CLI | Yes | Not yet validated | Expected to work | Smoke tested |
| Aider | Yes | Not yet validated | Expected to work | No smoke checklist yet |
| OpenCode | Yes | Not yet validated | Expected to work | No smoke checklist yet |

### Host Environment

| Dimension | Status |
|---|---|
| Minimum Go | 1.22.4 (from go.mod) |
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
