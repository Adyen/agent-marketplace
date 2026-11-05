## 1. Formula Generation

- [x] 1.1 Implement deterministic release-input validation and formula generation under `tools/homebrew`; verify exact stable tags, repository coordinates, matching unique asset metadata, all four local binary hashes, and invalid-input/no-output behavior with annotated Go tests (`formula-selects-each-supported-release-binary`, `invalid-release-data-produces-no-formula`).
- [x] 1.2 Add the Homebrew formula platform mapping, executable rename, license and Git dependency, and exact version test; verify all four OS/architecture selections, no-`v` formula metadata, and the installed binary's exact `vMAJOR.MINOR.PATCH` output with an annotated Ruby DSL-stub contract test (`formula-installs-the-declared-agent-marketplace-version`).

## 2. Authenticated and Anonymous Downloads

- [x] 2.1 Implement private immutable asset URLs and fetch-time token handling with safe header cleanup, plus the public anonymous release URL path; verify no token appears in formula or metadata, missing-token failure precedes a request, and public downloads need no token using annotated tests (`private-formula-defers-authentication-until-asset-fetch`, `public-formula-downloads-anonymously`).

## 3. Tap Publication Boundary

- [x] 3.1 Implement numeric `VERSION` comparison and idempotence rules; verify older releases are skipped, identical output is a no-op, same-version drift fails closed, and malformed state does not update the branch with annotated tests (`older-release-cannot-replace-a-newer-formula`, `identical-release-is-idempotent`, `same-version-output-conflict-fails-closed`, `malformed-tap-version-fails-closed`).
- [x] 3.2 Implement GitHub API tree, commit, and non-forcing ref publication for an absent or existing `homebrew` branch; verify only owned paths change, unrelated files and commit history are preserved, stale ref conflicts fail without moving the branch, and API failures are surfaced with an annotated fake-`gh` or local-HTTP test (`tap-branch-creation-and-update-preserve-unrelated-files`, `stale-publication-conflict-leaves-the-branch-unchanged`).

## 4. Stable Release Workflow

- [x] 4.1 Add a separate publication job dependent on successful `release` and stable classification; give the new job `contents: write` while preserving workflow-level `permissions: {}` and the existing release job's permission, plus a fixed non-canceling tap concurrency group, release metadata and binary retrieval, and no source-branch checkout or write; verify existing release assets and gates remain intact.
- [x] 4.2 Add workflow-structure tests for stable-only gating, successful release ordering, the publisher job's `contents: write`, unchanged workflow-level `permissions: {}`, preservation of the existing release permission, concurrency, and no source-branch writes; annotate the success and skip/failure cases (`successful-stable-release-publishes-after-binary-upload`, `non-stable-or-failed-release-does-not-publish`).

## 5. Verification and Archival

- [x] 5.1 Run `CGO_ENABLED=0 go test ./tools/homebrew ./tools/ci-plan`, `make validate TEST_PACKAGES='./tools/homebrew ./tools/ci-plan'`, and `make spec-check`; verify all new scenarios have matching annotations and, where a suitable CI runner exists, run the Homebrew smoke without relying on host-managed Homebrew.
- [x] 5.2 After implementation and required gates succeed, run `openspec archive add-homebrew-packaging --yes`; verify the capability spec is archived and the change status is complete.
