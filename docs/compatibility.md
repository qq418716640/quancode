# Compatibility Notes

QuanCode is an orchestration layer around third-party coding CLIs. Compatibility depends on both QuanCode behavior and the behavior of each upstream CLI.

When reading compatibility notes, distinguish between built-in adapter presence, CI coverage of the QuanCode repository itself, and manual smoke testing of specific paths. These are different evidence levels and should not be treated as equivalent guarantees.

## Scope of Current Compatibility

QuanCode currently ships built-in defaults for:

- Claude Code
- Codex CLI
- Aider
- OpenCode
- Qoder CLI

This means QuanCode knows how to route to them and has default adapter settings for startup or delegation. It does not mean every version of every CLI is fully validated.

## What Is Considered Supported Today

At the current alpha stage, "supported" means:

- the agent has a built-in adapter definition in `config/defaults.go`
- the command can be detected in `PATH`
- core startup or delegation flows are expected to work with recent CLI versions

## What Is Not Guaranteed Yet

- a full version-by-version compatibility matrix
- identical behavior across macOS and Linux for every third-party CLI version
- zero prompt-injection differences between CLIs
- production-stable guarantees for every isolation path

## Known Behavioral Differences

- Different CLIs use different prompt injection modes such as command args or a managed prompt file
- `prompt_mode=stdin` is not currently supported for primary interactive launch
- File-based prompt injection is managed by QuanCode and should restore the original file on primary exit
- Delegation output may depend on the upstream CLI's own output conventions

## Recommended Validation Before Release

- Run the checks in [`docs/manual-smoke-tests.md`](manual-smoke-tests.md)
- Record the tested OS, Go version, QuanCode version, and third-party CLI versions
- Treat failures as possibly CLI-version-specific until reproduced across environments

## Publishing Guidance

- Do not describe third-party compatibility as guaranteed unless it has been manually or automatically verified
- Prefer wording such as "validated with" or "tested against" over "fully supported"
- Keep the authoritative versioned release history in [`CHANGELOG.md`](../CHANGELOG.md)
