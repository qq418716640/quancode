# Releasing

This document describes the minimum release flow for QuanCode.

The goal is to produce versioned binaries with a version string that matches the git tag.

## Preconditions

- working tree is clean
- tests pass locally
- release notes are reflected in [`CHANGELOG.md`](../CHANGELOG.md)
- the release tag uses the same version format shown by `quancode version`

Current convention:

- tag format: `v0.1.0-alpha`
- binary version source: `github.com/qq418716640/quancode/version.Version`

## Local Verification

Before cutting a release:

```bash
go test ./...
go vet ./...
go run . version
```

Expected:

- tests pass
- `quancode version` prints the current development version or the injected tag value

## Release Artifacts

The repository includes a baseline [`.goreleaser.yml`](../.goreleaser.yml) configuration that:

- builds `quancode`
- targets Linux and macOS
- targets `amd64` and `arm64`
- injects the release tag into `version.Version`
- emits archive files and checksums
- can publish a Homebrew formula to `qq418716640/homebrew-tap` when `HOMEBREW_TAP_GITHUB_TOKEN` is available
- installs shell completions automatically for Homebrew users through the generated formula

The repository also includes [`.github/workflows/release.yml`](../.github/workflows/release.yml), which runs `goreleaser release --clean` on pushed `v*` tags.

## Homebrew Publishing Notes

The Homebrew path assumes a separate tap repository:

- tap repo: `qq418716640/homebrew-tap`
- formula path: `Formula/quancode.rb`
- required secret: `HOMEBREW_TAP_GITHUB_TOKEN`

Until that tap repository exists and the secret is configured in GitHub Actions, the Homebrew install flow documented in the README is still a release target rather than a currently verified distribution channel.

Once the tap repository is live, the intended user flow is:

```bash
brew tap qq418716640/tap
brew install quancode
```

The `brew tap` step only adds the QuanCode formula source. It does not replace the user's existing Homebrew sources.

## Example Release Flow

1. Update [`CHANGELOG.md`](../CHANGELOG.md)
2. Commit release-ready changes
3. Ensure the GitHub repository has `HOMEBREW_TAP_GITHUB_TOKEN` configured if Homebrew formula publishing should run
4. Create and push a tag such as `v0.1.0-alpha`
5. Wait for the `release` GitHub Actions workflow to finish

Example:

```bash
git tag v0.1.0-alpha
git push origin v0.1.0-alpha
```

## Notes

- This is now a release baseline with a Homebrew publishing scaffold, not a fully verified package-manager rollout
- The remaining manual step is creating and wiring the external tap repository
- If `HOMEBREW_TAP_GITHUB_TOKEN` is not configured, the release workflow should still publish normal release artifacts and skip Homebrew upload
- If the tag format changes, update `CHANGELOG.md`, this document, and any version parsing assumptions together
