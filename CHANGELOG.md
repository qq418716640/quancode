# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning in spirit, with alpha releases allowed to change behavior more quickly while the public interface settles.

## [Unreleased]

### Added

- Automatic project context injection for delegations (`CLAUDE.md`, `AGENTS.md`)
- `--context-files`, `--context-diff`, `--context-max-size`, and `--no-context` flags
- Post-delegation verification with `--verify` and `--verify-strict`
- `--verify-timeout` flag for verification command timeout
- `context_defaults` and per-agent `context` configuration in YAML config
- `FinalStatus` and verification results in ledger entries

### Changed

- `buildDelegationResult` now accepts an `attemptResult` struct instead of 11 parameters
- `applyPatch` failure in `worktree` mode is now an error instead of a warning
- Fallback rebuilding now regenerates the context bundle per agent so agent-specific `context` config is respected

## [v0.1.0-alpha] - 2026-03-27

First public alpha focused on making QuanCode publishable and usable by external developers.

### Added

- Primary-agent startup and delegation flows for the built-in adapters in `config/defaults.go`
- Managed file-based prompt injection for CLIs that need a prompt file such as `AGENTS.md`
- Backward-compatible config field backfills for older `quancode.yaml` files
- Initial open-source project assets:
  - `README.md`
  - `LICENSE`
  - `quancode.example.yaml`
  - GitHub Actions CI
- Initial automated coverage for:
  - config default backfills
  - prompt construction
  - router selection
  - prompt file restore behavior
  - delegate result shaping
  - environment merge semantics
  - changed-files helpers

### Changed

- File-based primary launch now restores managed prompt files after the primary CLI exits
- File-based primary launch preserves the child process exit status instead of collapsing non-zero exits to a generic failure
- CI now runs `go test ./...`, `go vet ./...`, and `go build ./...` on Linux and macOS

### Known Limitations

- Compatibility is still evolving across supported third-party CLIs and versions
- `prompt_mode=stdin` is not supported for primary interactive launch
- End-to-end smoke validation for all supported CLIs is still manual
- The recommended publication path is still a fresh repository without local history
