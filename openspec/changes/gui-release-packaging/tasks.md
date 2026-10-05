## 1. Unsigned package matrix

- [ ] 1.1 Build an unsigned `.dmg` and unsigned `.rpm` for every supported architecture; verify the `.app` is configured for the hardened runtime with notarization entitlements and registers `agent-marketplace`, while the RPM declares WebKitGTK and installs the URL-scheme desktop entry.
- [ ] 1.2 Publish checksums alongside the tagged-release GUI artifacts; verify one checksum for each supported package.
- [ ] 1.3 Produce the same unsigned package types and supported-architecture matrix in pull-request builds and run a launch smoke test; verify no organization signing identity or secret is used.

## 2. Downstream signability in CI

- [ ] 2.1 In every pull-request and tagged-release build, ad-hoc sign a copy of the `.app` with the hardened runtime and verify it using `codesign --verify --strict`; verify the published `.app` itself remains unsigned.
- [ ] 2.2 In every pull-request and tagged-release build, generate a throwaway GPG key, sign a copy of the `.rpm` using `rpm --addsign`, and verify it with `rpm -K`; verify the key is not retained after the job.
- [ ] 2.3 Annotate the CI artifact-copy signability checks to the downstream-signability requirements; verify these checks use no Developer ID, organization identity, or organization signing secret.
- [ ] 2.4 Audit repository configuration and workflows for organization signing identities, Apple team IDs, key locations, and signing secrets; verify none are present.

## 3. CLI safeguards and finalization

- [ ] 3.1 Keep the CLI release statically linked and unchanged; compare CLI artifact metadata and checksums before and after adding GUI packaging.
- [ ] 3.2 Run release/package checks and `make spec-check`; require zero trace findings for this layer, with no unbound scenarios or dangling annotations.
- [ ] 3.3 Verify no `.deb`, AppImage, auto-update, or Windows artifacts enter the package matrix.
- [ ] 3.4 Run `openspec archive gui-release-packaging --yes` in this layer's pull request.
