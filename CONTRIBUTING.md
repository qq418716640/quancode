# Contributing

Thanks for contributing to QuanCode.

This document describes the minimum expectations for contributions. It intentionally avoids requiring any specific AI CLI, proxy setup, shell customization, or personal workflow.

## Before You Start

- Read [`README.md`](README.md) for the project overview and setup flow
- Read [`docs/compatibility.md`](docs/compatibility.md) if your change affects supported CLIs or compatibility claims
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

If you change agent configuration behavior:

- keep adapter logic data-driven where possible
- update [`quancode.example.yaml`](quancode.example.yaml) if the user-facing config shape changes
- update [`docs/agent-config-schema.md`](docs/agent-config-schema.md) when config fields or semantics change

### Delegation and startup changes

If you change startup, delegation, or isolation behavior:

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

If your change affects primary startup or delegation behavior, also run the relevant steps from [`docs/manual-smoke-tests.md`](docs/manual-smoke-tests.md) when practical.

## Reporting Bugs

When filing a bug, include:

- operating system
- Go version
- QuanCode version from `quancode version`
- affected third-party CLI name and version
- whether you used a custom config or `quancode.example.yaml`
- exact reproduction steps

Use the issue templates in GitHub when possible.
