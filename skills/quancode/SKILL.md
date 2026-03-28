---
name: quancode
description: Orchestrate QuanCode sub-agent delegation from Claude Desktop, Cowork, or Dispatch. Routes coding tasks to codex, qoder, claude, or another enabled QuanCode agent through `quancode delegate`. Use when the user wants to hand off a bounded coding task to another AI agent.
argument-hint: "[--agent codex|qoder|claude] task description"
allowed-tools: Bash(quancode *)
---

# QuanCode Skill

Use this skill when the user wants to delegate a bounded coding task to a QuanCode-managed agent.

## Core Rule

Always invoke sub-agents through `quancode delegate`. Never call agent CLIs directly.

## Preconditions

Before delegating, confirm:
- `quancode` is available on PATH
- the current folder is the intended project, or the user gave a repo path

If repo context is unclear (especially from Dispatch/mobile), ask one short question:
- "Which repo should I run this in?"

## Intent Parsing

Explicit delegation (user names an agent):
- "delegate to codex: write tests" → `--agent codex`
- "ask qoder to review this" → `--agent qoder`
- "have claude design the API" → `--agent claude`

Implicit delegation (auto-route):
- "write tests for auth" → omit `--agent`, let QuanCode route
- "review this patch" → auto-route
- "explain the runner package" → auto-route

## Isolation Rules

`--isolation inplace` (default): explanation, review, read-only analysis, small edits

`--isolation worktree`: writing tests, refactors, code generation, multi-file edits

`--isolation patch`: "show me the patch", mobile/Dispatch requests, risky edits

If worktree/patch is needed but the folder is not a git repo, fall back to inplace.

## Execution

Always use `--format json` to inspect results structurally:

```
quancode delegate --workdir "$(pwd)" --format json "<task>"
quancode delegate --agent codex --workdir "$(pwd)" --isolation worktree --format json "write tests for auth"
```

## Parallel Delegation

Allowed only when tasks are clearly independent and do not edit the same files. Max 2-3 parallel tasks. Prefer worktree or patch isolation.

Good: one agent writes tests while another reviews a separate package.
Bad: two agents both editing the same module.

## Result Handling

After delegation:
1. Inspect `status`, `exit_code`, `timed_out`, `changed_files` from JSON
2. Summarize what the sub-agent did in natural language
3. Mention changed files when relevant
4. Call out failures or fallback clearly
5. Do not dump raw JSON unless asked

## Dispatch / Mobile Quick Reference

From your phone, these terse prompts work:

- "fix the login bug"
- "write tests for config"
- "codex: refactor the helper"
- "qoder: review auth module"
- "explain how routing works"
- "patch: rewrite the README intro"

The skill infers agent selection and isolation mode from context.

## Agent Selection Hints

When auto-routing is not explicit:
- `codex` — quick edits, code generation, test writing
- `qoder` — explanation, review, code analysis
- `claude` — architecture, design, complex reasoning

But prefer letting QuanCode auto-route unless the user specifies.
