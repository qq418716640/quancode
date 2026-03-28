# Privacy Notes

QuanCode is a local CLI orchestrator. This document describes the privacy posture of QuanCode itself, not the policies of third-party coding CLIs or their upstream services.

## What QuanCode Itself Does Not Currently Do

QuanCode does not currently include built-in:

- telemetry collection
- analytics event reporting
- cloud-hosted log upload
- remote account management

## What QuanCode Does Process Locally

Depending on the command you run, QuanCode may locally process:

- working directory paths
- task descriptions passed to `quancode start` or `quancode delegate`
- command output from third-party CLIs
- local config files
- git status and patch information
- local ledger or quota files written under the user's config directory

## Third-Party CLI Behavior

QuanCode can launch or delegate to third-party coding CLIs such as Claude Code, Codex CLI, Aider, or OpenCode.

Those tools may:

- make their own network requests
- use their own authentication flows
- read workspace files
- log data according to their own product behavior

QuanCode does not control or override the privacy policies of those tools or their providers.

## Local Workspace Implications

- delegated tasks may cause upstream CLIs to read or modify files in the working directory
- file-based prompt injection may temporarily write managed prompt content to a workspace file such as `AGENTS.md`
- logs and quota data may be written locally for operational purposes

## Practical Guidance

- review the privacy and data-handling policies of any third-party CLI you enable
- avoid putting secrets in issue reports, screenshots, or shared config examples
- inspect your local config before sharing it publicly
- treat workspace content passed through third-party CLIs as subject to those tools' own behavior
