# RFC: Delegation Approval Handshake

Status: implemented (interactive approval added in v0.1.1)

## Summary

This RFC proposes a minimal approval-handshake mechanism for delegated sub-agent execution in QuanCode.

Today, `quancode delegate` is effectively fire-and-forget: it starts a sub-agent CLI, waits for completion, and returns the final result. That model works for non-interactive one-shot tasks, but it does not support a delegated sub-agent pausing to request approval before a risky action.

The goal of this RFC is to define the smallest coherent design that:

- lets a delegated sub-agent request approval during execution
- lets the primary agent or user approve or deny that request
- remains backward-compatible with existing non-interactive delegation
- does not require a daemon, server, or interactive TUI layer

## Problem

QuanCode currently assumes that delegated sub-agents can run to completion without additional human interaction.

That assumption breaks down when a sub-agent reaches an action that should require approval, such as:

- force-pushing a branch
- deleting or overwriting important files
- editing CI or deployment configuration
- making network or API calls with side effects

Today, if a delegated CLI needs interactive approval, the result is usually one of these:

- it exits immediately with an error
- it blocks waiting for input that QuanCode never provides
- it times out and fails

This is acceptable for the current early-alpha one-shot model, but it is not a good long-term delegation experience.

## Non-Goals

This RFC does not propose:

- a policy engine for auto-approving or auto-denying actions
- a background daemon or long-lived service
- a GUI or TUI approval interface
- an attempt to infer approval requests from arbitrary unstructured CLI output
- mandatory support across all agents; unsupported agents should continue to run as they do today

## Design Constraints

Any approval-handshake design should satisfy these constraints:

- Delegated sub-agents are opaque third-party CLI processes. QuanCode does not control their internals.
- Existing delegation flows must continue to work unchanged for agents that do not support approval handshake.
- The design should fit QuanCode's current execution model: local CLI, environment injection, temp files, and no server process.
- The handshake should avoid hijacking the sub-agent's stdin/stdout in ways that break existing prompt or output modes.
- The resulting protocol should be simple enough to wrap around current adapters incrementally.

## Proposed Model

The minimal model is:

1. `quancode delegate` starts a delegated execution as usual.
2. QuanCode provides the delegated process with a temporary approval directory via environment variable.
3. If the delegated process or its wrapper needs approval, it writes a structured JSON request file into that directory.
4. QuanCode surfaces that request as a pending approval event.
5. A separate `quancode approve` command writes a structured JSON response file for that request.
6. The delegated process resumes or aborts based on that response.

This design uses:

- environment variable discovery
- filesystem-based IPC
- JSON request/response files

It deliberately avoids sockets, daemons, or bidirectional interactive streams.

## Command Surface

This RFC proposes one new command:

```bash
quancode approve <request-id> [--allow | --deny] [--reason "..."]
```

Behavior:

- exactly one of `--allow` or `--deny` must be provided
- `--allow` approves the pending request
- `--deny` denies the pending request
- `--reason` is optional metadata recorded with the decision

The delegated process does not call the primary CLI directly. Instead, it depends on QuanCode's approval directory and response file.

The primary agent can:

- surface the pending request to the user
- make a decision itself
- instruct the user to run `quancode approve ...`

This RFC does not introduce a separate `quancode approvals` listing command yet. That can be added later if the minimal workflow proves too awkward. In the minimal design, `quancode delegate` must surface the pending `request_id` in text and JSON output so that `quancode approve <request-id>` is actionable.

## Environment Contract

When an approval-capable delegated execution starts, QuanCode should inject:

```text
QUANCODE_DELEGATION_ID=<opaque-id>
QUANCODE_APPROVAL_DIR=<temp-dir>
```

Example:

```text
QUANCODE_DELEGATION_ID=del_01H...
QUANCODE_APPROVAL_DIR=$TMPDIR/quancode-approval-del_01H...
```

The delegated process or wrapper can discover the approval channel from these variables.

## Filesystem IPC Layout

For one delegated execution, QuanCode creates a dedicated directory:

```text
$TMPDIR/quancode-approval-<delegation-id>/
```

Inside that directory:

- approval requests are written as `request-<request-id>.json`
- approval responses are written as `response-<request-id>.json`

Example:

```text
/tmp/quancode-approval-del_123/
  request-req_1.json
  response-req_1.json
  request-req_2.json
```

The delegated side may poll for response files. This RFC does not require a more advanced notification mechanism.

Recommended polling interval for wrappers or delegated helpers:

- default: 1 second
- acceptable range: 1 to 2 seconds

Writers should write JSON to a temporary file in the same directory and then atomically rename it into place. Readers should only consume final `request-*.json` and `response-*.json` files, not temporary files.

Cleanup timing is intentionally left open in this draft. At minimum, implementations need a defined strategy for:

- normal cleanup after delegation completes
- cleanup after deny or timeout
- best-effort cleanup after abnormal process exit

## State Machine

The minimal approval state machine is:

```text
RUNNING -> PENDING -> APPROVED -> RUNNING
RUNNING -> PENDING -> DENIED
RUNNING -> PENDING -> TIMED_OUT_DENIED
RUNNING -> PENDING -> FAILED
RUNNING -> COMPLETED
RUNNING -> FAILED
```

Interpretation:

- `RUNNING`: delegated process is executing normally
- `PENDING`: an approval request has been raised and execution is waiting
- `APPROVED`: the current request was approved; execution may continue
- `DENIED`: the current request was denied; delegated action should abort
- `TIMED_OUT_DENIED`: no decision arrived before timeout; treated as deny
- `FAILED`: delegated process exited unsuccessfully, including while waiting on approval
- `COMPLETED`: delegated process finished successfully

Rules:

- A single delegation may raise multiple approval requests, but only one should be pending at a time in the minimal design.
- Pending approvals should have a timeout. A reasonable starting point is 120 seconds, configurable later if needed.
- Timeout should behave like deny, not like silent success.
- Timeout should be enforced by QuanCode, which writes a denial response file with `decided_by: "timeout"`. The delegated side should react to the response file rather than invent its own timeout decision.

## JSON Schema

### Approval Request

Written by the delegated side:

```json
{
  "schema_version": 1,
  "request_id": "req_123",
  "delegation_id": "del_123",
  "timestamp": "2026-03-27T12:34:56Z",
  "action": "git_push_force",
  "description": "Force-push branch 'feature-x' to origin",
  "risk_level": "high",
  "context": {
    "agent": "codex",
    "working_dir": "/path/to/repo",
    "files_affected": ["README.md", ".github/workflows/ci.yml"]
  }
}
```

Field notes:

- `schema_version` allows future protocol evolution
- `request_id` must be unique within the delegation
- `action` is free-form text in this RFC; it is not standardized yet
- `risk_level` is optional advisory metadata intended for human or primary-agent decision support only
- `context` is optional and agent-specific; consumers must treat it as an open object with no fixed required subfields

### Approval Response

Written by QuanCode in response:

```json
{
  "schema_version": 1,
  "request_id": "req_123",
  "decision": "approved",
  "reason": "confirmed by user",
  "decided_by": "user",
  "timestamp": "2026-03-27T12:35:10Z"
}
```

Valid `decision` values:

- `approved`
- `denied`

Valid `decided_by` examples:

- `user`
- `primary`
- `timeout`

For v1, `decided_by` should be treated as an open string field. The values above are recommended initial values, not a closed enum.

## Delegate Output Behavior

This RFC intentionally leaves room for how `quancode delegate` exposes pending approvals, but the minimum expectation is:

- text mode should surface a clear pending-approval message
- JSON mode should surface a structured pending-approval result

Conceptually, a JSON response could look like:

```json
{
  "status": "waiting_approval",
  "agent": "codex",
  "task": "update deployment workflow",
  "request": {
    "request_id": "req_123",
    "action": "git_push_force",
    "description": "Force-push branch 'feature-x' to origin"
  }
}
```

This RFC does not lock the final JSON envelope yet. It only requires that pending approval not be reported as a generic opaque failure.

## Ledger Integration

Approval activity should eventually be recorded in the ledger for auditability.

The minimal extension would be an optional `approval_events` field on a ledger entry, containing:

- `request_id`
- `action`
- `decision`
- `timestamp`

This can be added in a later implementation step without changing the core handshake protocol.

## Backward Compatibility

Backward compatibility is required:

- agents that do not support approval handshake continue to work exactly as they do today
- existing `delegate_args`, `task_mode`, and `output_mode` semantics remain unchanged
- approval handshake is opt-in at the adapter or wrapper level

If no approval request is ever emitted, the delegated run behaves like a normal current delegation.

## Suggested Implementation Phases

This RFC is intentionally staged.

Phase 1:

- inject `QUANCODE_DELEGATION_ID`
- inject `QUANCODE_APPROVAL_DIR`
- add `quancode approve`
- implement request/response file handling primitives

Phase 1 is infrastructure only. It does not provide a complete end-to-end approval workflow until Phase 2 teaches `quancode delegate` to detect and surface pending approval requests.

Phase 2:

- let `quancode delegate` detect pending approval requests
- emit pending-approval status in text and JSON modes
- support resume/continue behavior after approval response

Phase 3:

- record approval activity in ledger
- consider additional commands such as approval listing or history

## Open Questions

- Should pending approvals be surfaced as a new top-level delegate status, or as an error with structured metadata?
- Should approval timeout be global, per-agent, or per-request-type?
- Should QuanCode itself ever auto-deny based on static policy, or should that remain outside the first implementation?
- What is the smallest useful wrapper pattern for existing agents like Codex or Claude Code to participate in this handshake?
- Should the minimal CLI surface eventually add a pending-approval listing command, or is delegate output alone enough?
- What cleanup policy should QuanCode adopt for approval directories in normal and abnormal exit paths?

## Recommendation

Adopt this RFC as a draft design target, not an implementation promise.

The important part is to standardize:

- the environment contract
- the request/response file format
- the minimal state machine

That gives QuanCode a realistic path to supporting approval-aware delegation without abandoning its current local, config-driven CLI architecture.
