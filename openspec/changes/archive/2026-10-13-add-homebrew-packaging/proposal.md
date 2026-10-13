## Why

Users currently must obtain Agent Marketplace binaries outside Homebrew, which makes repeatable installation and upgrades less convenient across supported macOS and Linux platforms. A same-repository tap provides a stable-release installation path for both public consumers and the currently private repository’s authorized users without changing the existing release binaries or gates.

## What Changes

- Generate a deterministic Homebrew formula for each stable `vX.Y.Z` release from the four existing macOS/Linux arm64/amd64 binaries, with checksums, version-aware installation, and no unsupported-platform fallback.
- Publish the formula to a dedicated `homebrew` branch in the existing repository only after the current stable release succeeds; preserve unrelated branch content and reject stale or conflicting publication.
- Support anonymous canonical release downloads when the repository is public and authenticated immutable GitHub asset downloads when it is private, without embedding credentials in formula artifacts.
- Add generator, formula-contract, publication-boundary, and workflow-structure tests.

## Capabilities

### New Capabilities
- `homebrew-packaging`: Stable-release formula generation, platform installation, private asset authentication, and safe publication to the same-repository tap branch.

### Modified Capabilities
- None.

## Impact

- Adds a Go generator and tests under `tools/homebrew/`, plus a stable-release-only publication job in `.github/workflows/ci.yml`.
- Publishes only the Homebrew formula and any required download strategy or version metadata on the `homebrew` branch; existing source branches, release assets, binary names, and release gates remain unchanged.
- Uses the existing repository’s GitHub API and per-job `GITHUB_TOKEN` permissions; no separate tap repository, new secret, or host Homebrew configuration is required.
