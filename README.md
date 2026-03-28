# QuanCode

[中文](README_zh.md)

QuanCode is a CLI orchestrator for terminal coding agents. It starts one AI coding CLI as the primary interface and lets it delegate bounded tasks to other coding CLIs.

It is an orchestration layer, not an agent itself.

Use it when you want one terminal workflow that can hand off bounded tasks to the most suitable coding CLI without constant manual switching.

> **Status: early alpha**  
> Core flows work on tested configurations, but the agent matrix and prompt-injection behavior are still changing. Expect rough edges.

## Install

Prerequisites:

- Go 1.25+
- At least one supported coding CLI installed and authenticated

Install from source:

```bash
go install github.com/qq418716640/quancode@latest
```

Check the installed version:

```bash
quancode version
```

Alternative install paths:

- Coming soon:

  ```bash
  brew tap qq418716640/tap
  brew install quancode
  ```

  Tapping only adds the QuanCode formula source. It does not replace other Homebrew sources.
- Local build: `git clone https://github.com/qq418716640/quancode.git && cd quancode && go build -o quancode .`

## Quick Start

1. Detect installed CLIs and generate a config:

```bash
quancode init
```

2. Verify setup:

```bash
quancode doctor
```

3. Start a primary agent:

```bash
quancode start
quancode start --primary codex
```

The `--primary` and `--agent` flags support tab-completion for agent names.

## What It Does

- Starts a primary coding CLI with delegation instructions injected via CLI args, env vars, or a managed file.
- Delegates one-shot tasks to other coding CLIs and returns text or JSON output.
- Routes tasks by keyword match and static priority. It does not do LLM-based routing.
- Supports in-place execution, isolated git worktrees, or patch-only delegation.
- Logs delegation calls to JSONL and supports optional per-agent quota limits.
- Auto-fallback: if an agent times out or hits a rate limit, QuanCode automatically retries with the next available agent. Disable with `--no-fallback`.

## Configuration

Config search order:

1. `--config <path>`
2. `./quancode.yaml`
3. `~/.config/quancode/quancode.yaml`
4. built-in defaults

Minimal example:

```yaml
default_primary: claude

agents:
  claude:
    name: Claude Code
    command: claude
    enabled: true
    primary_args: ["--append-system-prompt"]

  codex:
    name: Codex CLI
    command: codex
    enabled: true
    prompt_mode: file
    prompt_file: AGENTS.md
    delegate_args: ["exec", "--full-auto", "--ephemeral"]
    output_flag: --output-last-message
```

For a fuller starter config without local proxy or machine-specific assumptions, copy [`quancode.example.yaml`](quancode.example.yaml).

Field-by-field config documentation is available in [`docs/agent-config-schema.md`](docs/agent-config-schema.md).

## Usage

See the [`User Guide`](docs/user-guide.md) for command-by-command walkthroughs, isolation mode guidance, and troubleshooting.

## Supported Agents

Built-in defaults currently cover:

- Claude Code
- Codex CLI
- Qoder CLI (code-analysis, debugging, explanation, MCP integration)
- Aider
- OpenCode

Support is adapter-based rather than hardcoded per command path. Different CLIs may use different prompt injection modes such as CLI args, env vars, or a managed file like `AGENTS.md`.

Coverage is not uniform across adapters. Claude Code currently has the most validation; other built-in adapters have less test and smoke coverage.

A `/quancode` skill is available for Claude Desktop, Cowork, and Dispatch, enabling multi-agent delegation from those environments.

QuanCode is an independent project. Compatibility may vary by CLI version.

For current compatibility expectations and non-goals, see [`docs/compatibility.md`](docs/compatibility.md).

For a conservative status table of current adapter confidence, see [`docs/compatibility-matrix.md`](docs/compatibility-matrix.md).

## Safety Notes

- Use `--auto-approve` to skip confirmation prompts during delegation.
- Delegated agents run in your working directory unless you use an isolation mode.
- `--isolation worktree` and `--isolation patch` require a git repository.
- File-based prompt injection is managed by QuanCode and should restore original content after the primary exits.
- Review sub-agent changes before committing them.

## Development

Run the standard checks:

```bash
go test ./...
go vet ./...
```

Release builds can override the default version string with Go ldflags. The release tag should be treated as the final source of truth.

Project entry points:

- `cmd/start.go`: primary startup
- `cmd/delegate.go`: sub-agent execution
- `cmd/apply_patch.go`: patch application for parallel delegation
- `agent/agent.go`: generic agent adapter
- `prompt/injection.go`: primary prompt construction
- `router/router.go`: agent selection
- `runner/`: execution and isolation helpers
- `ledger/`: logs and quotas

## Documentation

- User guide: [`docs/user-guide.md`](docs/user-guide.md)
- Config reference: [`docs/agent-config-schema.md`](docs/agent-config-schema.md)
- Compatibility: [`docs/compatibility.md`](docs/compatibility.md)
- Privacy: [`docs/privacy.md`](docs/privacy.md)
- Contributing: [`CONTRIBUTING.md`](CONTRIBUTING.md)
- Changelog: [`CHANGELOG.md`](CHANGELOG.md)

## License

Apache-2.0. See [LICENSE](LICENSE).
