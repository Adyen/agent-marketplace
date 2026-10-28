## Context

See proposal.md for motivation. GUI packages add a separate cgo desktop application while the existing CLI release remains statically linked. The public repository cannot hold the organization's Developer ID or RPM signing identities.

## Goals / Non-Goals

**Goals:** Publish unsigned macOS/Linux GUI packages that organizational distributors can sign and notarize downstream without rebuilding; validate that path in CI without organization signing material.

**Non-Goals:** `.deb`, AppImage, auto-update, Windows, organization signing identities in public CI, or changes to CLI artifacts and their static-link behavior.

## Decisions

- Build a hardened-runtime macOS `.app` with the entitlement declaration required by downstream notarization, but do not sign the public `.dmg` or app with an organization identity. Publish one unsigned `.dmg` and one unsigned `.rpm` per supported architecture with checksums.
- The RPM declares its WebKitGTK runtime dependency and installs a desktop entry so the app appears in desktop launchers. URL-scheme registration is out of scope until the deferred `gui-install-deep-link` change adds a handler.
- Treat tagged releases as public unsigned distribution. An organization's downstream macOS software-distribution pipeline can sign the published app with its Developer ID using `codesign` and hardened runtime, then notarize it. An internal RPM signing and publishing pipeline can sign the published RPM with `rpm --addsign`. These pipelines and identities remain outside this repository.
- Prove signability for every pull-request and tagged-release build using copies of the artifacts: ad-hoc sign the app with hardened runtime and pass `codesign --verify --strict`; generate a throwaway GPG key in CI, sign the RPM with `rpm --addsign`, and pass `rpm -K`. Discard the temporary key after the job.
- Pull-request builds publish the same unsigned package types and architecture matrix and run a launch smoke test. Preserve the existing CLI build/link and release steps separately.
- **Accepted trade-off:** Public GitHub downloads are unsigned, so macOS Gatekeeper warns. Non-terminal users at an organization are expected to receive their signed build from their organization's distributor.

## Risks / Trade-offs

- [An administrator enables `gui.version_check` before every managed client can read it] → v4 decoding currently rejects unknown top-level keys with `v4.unknown-field` in `internal/config/v4.go`; the strict legacy v2/v3 decoder uses `KnownFields(true)` in `internal/config/v3.go`. Older clients would reject an organization config containing `gui`. Ship CLI read support in a release first. Release notes and administrator documentation SHALL tell organizations to deploy `gui.version_check` only after every managed client runs a release that reads it.
- [A GUI build changes CLI release bytes or linkage] → Keep GUI packaging jobs separate and compare CLI artifact metadata and checksums.
- [A supported architecture is missing from the build matrix] → Derive the matrix from the repository's supported architecture list and fail when any target lacks an artifact.
- [A signing validation accidentally modifies the public unsigned artifact] → Sign copies in CI; publish only the original unsigned packages.
