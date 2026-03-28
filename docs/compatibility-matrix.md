# Compatibility Matrix

This matrix is intentionally conservative. It describes the current confidence level of QuanCode's own adapter layer, not a blanket guarantee about every upstream CLI version.

Evidence levels used here:

- built-in adapter: QuanCode ships a default config for the CLI
- CI covered: the repository currently runs automated checks on that host environment
- manual smoke tested: the current repository includes explicit manual validation steps for that path
- not yet validated: present in code, but not yet covered by current smoke guidance

For the narrative explanation behind these labels, see [`docs/compatibility.md`](compatibility.md).

CI coverage does not mean every third-party CLI behavior has been validated on that OS. It only means the QuanCode repository itself currently builds and passes automated checks there.

## Host Environment

| Dimension | Current evidence | Notes |
|---|---|---|
| Minimum supported Go | `go.mod` currently declares Go 1.22.4 | This is the repository baseline, not a promise about every patch version |
| CI-tested Go | Go version from `go.mod` via `actions/setup-go` | Current CI uses the `go.mod` version file rather than an expanded Go version matrix |
| CI-covered OS | Linux and macOS | Current CI runs on `ubuntu-latest` and `macos-latest` |
| Manual smoke-tested OS paths | Current manual smoke guidance is written for generic local environments | The repository does not yet maintain a strict OS-by-CLI smoke matrix |

## Current Matrix

| CLI | Built-in adapter | Primary start path | Delegate path | Manual smoke-tested notes |
|---|---|---|---|---|
| Claude Code | Yes | Expected to work | Expected to work | Covered in [`docs/manual-smoke-tests.md`](manual-smoke-tests.md) |
| Codex CLI | Yes | Expected to work | Expected to work | Covered in [`docs/manual-smoke-tests.md`](manual-smoke-tests.md) |
| Aider | Yes | Not yet validated | Expected to work | No dedicated smoke checklist yet |
| OpenCode | Yes | Not yet validated | Expected to work | No dedicated smoke checklist yet |
| Qoder CLI | Yes | Not yet validated | Expected to work | Covered in [`docs/manual-smoke-tests.md`](manual-smoke-tests.md) |

## Important Limits

- This is not a version-by-version certification table
- Different upstream CLI releases may change behavior independently of QuanCode
- Operating-system differences may still exist even with current Linux/macOS CI coverage
- Some behaviors depend on git availability, working tree state, and local CLI authentication

## Guidance For Users

- Treat `manual smoke tested` as stronger evidence than `built-in adapter` alone
- Validate the exact CLI versions you plan to use in your own environment
- If you find a compatibility gap, include CLI name, CLI version, OS, and `quancode version` in the issue report
