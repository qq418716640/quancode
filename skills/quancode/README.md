# QuanCode Skill for Claude Desktop / Cowork / Dispatch

A skill that lets Claude delegate coding tasks to QuanCode-managed sub-agents (Codex, Qoder, Claude, etc.) directly from Claude Desktop, Cowork, or your phone via Dispatch.

## Install

### Prerequisites

- [QuanCode](../../README.md) installed and on PATH (`brew install qq418716640/tap/quancode`)
- At least one sub-agent CLI installed (codex, qodercli, etc.)
- Run `quancode doctor` to verify setup

### Copy the skill

```bash
cp -r skills/quancode ~/.claude/skills/quancode
```

Or symlink for automatic updates:

```bash
ln -s "$(pwd)/skills/quancode" ~/.claude/skills/quancode
```

## Usage

### In Claude Desktop / Cowork

```
/quancode write tests for the auth module
/quancode --agent codex refactor the helper and update tests
/quancode --isolation patch rewrite the README opening paragraph
```

### From phone via Dispatch

Send terse messages:

```
fix the login bug
codex: write tests for config
review the auth module
```

The skill infers the right agent and isolation mode from context.

### Parallel delegation

```
/quancode codex: write tests for router AND qoder: review the fallback logic
```

## How it works

1. You invoke `/quancode` with a task description
2. Claude parses your intent (agent, isolation mode, task)
3. Claude runs `quancode delegate --format json ...`
4. QuanCode routes to the best agent, handles approval, fallback, and isolation
5. Claude summarizes the result back to you

All sub-agent execution goes through `quancode delegate` — never directly calling CLI tools. This ensures proper auth, proxy, logging, quota tracking, and fallback.
