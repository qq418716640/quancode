# Contributing

Thanks for contributing to QuanCode.

## Before You Start

- Read [`README.md`](README.md) for the project overview and setup flow
- Read [`docs/compatibility.md`](docs/compatibility.md) if your change affects supported CLIs
- Prefer small, reviewable pull requests over large mixed changes

## Development Setup

Prerequisites:

- Go 1.22+
- git

Optional, depending on what you are changing:

- one or more supported coding CLIs installed locally

Basic setup:

```bash
git clone https://github.com/qq418716640/quancode.git
cd quancode
go test ./...
go vet ./...
```

## What To Include In A Pull Request

Minimum expectations:

- the change has a clear purpose
- `go test ./...` passes
- relevant tests or docs are updated when behavior changes
- the PR description explains what changed and why

Recommended:

- keep the scope narrow
- mention any manual validation you ran
- call out compatibility impact if the change affects CLI adapters or startup/delegation behavior

## Project-Specific Guidance

### Config and agent changes

- keep adapter logic data-driven where possible
- update [`quancode.example.yaml`](quancode.example.yaml) if the user-facing config shape changes
- update [`docs/agent-config-schema.md`](docs/agent-config-schema.md) when config fields or semantics change

### Delegation and startup changes

- update tests when possible
- avoid undocumented changes to machine-readable JSON output
- keep stderr diagnostic and stdout automation-friendly

### Documentation changes

- keep public docs conservative
- do not claim broad compatibility unless it has been tested
- avoid machine-specific assumptions such as local proxy settings, shell aliases, or personal paths

## Testing

Run the standard checks before opening a PR:

```bash
go test ./...
go vet ./...
```

### Manual Smoke Tests

If your change affects primary startup or delegation behavior, run the relevant smoke tests:

**Preconditions:**

```bash
cp quancode.example.yaml /tmp/quancode-smoke.yaml
go install .
quancode --config /tmp/quancode-smoke.yaml doctor
```

**Config and agent listing:**

```bash
quancode --config /tmp/quancode-smoke.yaml doctor
quancode --config /tmp/quancode-smoke.yaml agents
```

**Claude Code:**

```bash
quancode --config /tmp/quancode-smoke.yaml start --primary claude
quancode --config /tmp/quancode-smoke.yaml delegate --agent claude --format text "summarize the router package"
```

**Codex CLI:**

```bash
quancode --config /tmp/quancode-smoke.yaml start --primary codex
quancode --config /tmp/quancode-smoke.yaml delegate --agent codex --format json "list the main packages in this repo"
```

**Qoder CLI:**

```bash
quancode --config /tmp/quancode-smoke.yaml delegate --agent qoder "explain what this project does"
```

**Patch isolation (in a git repo):**

```bash
quancode --config /tmp/quancode-smoke.yaml delegate --agent codex --isolation patch "make a tiny comment-only change"
```

## Releasing

Release flow:

1. Ensure working tree is clean and tests pass
2. Create and push a version tag:

```bash
git tag v0.2.1
git push origin v0.2.1
```

3. The `release` GitHub Actions workflow runs GoReleaser automatically
4. GoReleaser builds binaries, publishes GitHub release, and updates the Homebrew tap

Details:

- Version is injected into the binary via ldflags from the git tag
- Homebrew formula is auto-published to `qq418716640/homebrew-tap`
- Shell completions are installed automatically for Homebrew users
- Required GitHub secret: `HOMEBREW_TAP_GITHUB_TOKEN`

## Reporting Bugs

When filing a bug, include:

- operating system
- Go version
- QuanCode version from `quancode version`
- affected third-party CLI name and version
- exact reproduction steps
