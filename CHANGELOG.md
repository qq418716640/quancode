# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning in spirit, with alpha releases allowed to change behavior more quickly while the public interface settles.

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
