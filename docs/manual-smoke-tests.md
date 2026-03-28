# Manual Smoke Tests

This checklist is for pre-release manual validation of the alpha CLI.

Scope:

- primary startup
- one-shot delegation
- prompt injection / restore behavior
- basic operator-visible output

These are smoke tests, not exhaustive compatibility certification.

## Preconditions

- Start from a clean clone or fresh working directory
- Install `quancode` from the current source tree
- Install and log in to the target coding CLIs you want to validate
- Copy `quancode.example.yaml` to a local config file and adjust only what your machine actually needs
- If testing file-based prompt injection, ensure you know whether the target workspace already contains `AGENTS.md`

Example setup:

```bash
cp quancode.example.yaml /tmp/quancode-smoke.yaml
go install .
quancode --config /tmp/quancode-smoke.yaml doctor
```

## Generic Checks

### 1. Config loads

```bash
quancode --config /tmp/quancode-smoke.yaml doctor
```

Expected:

- config loads successfully
- enabled agents appear as available if installed

### 2. Agent listing works

```bash
quancode --config /tmp/quancode-smoke.yaml agents
```

Expected:

- enabled agents are listed
- command names and availability statuses are readable

## Claude Code Smoke Tests

### 3. Claude as primary

```bash
quancode --config /tmp/quancode-smoke.yaml start --primary claude
```

Expected:

- Claude starts interactively
- the session is aware that other agents can be delegated through `quancode delegate`
- no unexpected usage/error banner appears during normal startup

### 4. Delegate a one-shot task to Claude

```bash
quancode --config /tmp/quancode-smoke.yaml delegate --agent claude --format text "summarize the router package"
```

Expected:

- command exits successfully
- output is plain text
- no local machine assumptions leak into the response

## Codex Smoke Tests

### 5. Codex as primary

```bash
quancode --config /tmp/quancode-smoke.yaml start --primary codex
```

Expected:

- Codex starts interactively
- startup logs mention prompt injection into `AGENTS.md`
- after exit, `AGENTS.md` content is restored or removed if it did not exist before

### 6. Delegate a one-shot task to Codex in JSON mode

```bash
quancode --config /tmp/quancode-smoke.yaml delegate --agent codex --format json "list the main packages in this repo"
```

Expected:

- command prints valid JSON
- JSON includes `agent`, `task`, `exit_code`, `timed_out`, `duration_ms`, and `output`

## Qoder CLI Smoke Tests

### 7. Delegate a one-shot task to Qoder

```bash
quancode --config /tmp/quancode-smoke.yaml delegate --agent qoder "explain what this project does"
```

Expected:

- command exits with code 0
- output is non-empty plain text
- no unexpected error messages appear

## Optional Isolation Checks

### 8. Patch isolation

Run this inside a git repository:

```bash
quancode --config /tmp/quancode-smoke.yaml delegate --agent codex --isolation patch "make a tiny comment-only change"
```

Expected:

- command returns a patch instead of directly editing the working tree
- the working tree remains unchanged unless you apply the patch yourself

## Alpha Notes

- Failures may still be CLI-version-specific rather than purely QuanCode bugs
- Record the tested CLI names and versions alongside any issue report
- Do not treat this checklist as proof of production readiness
