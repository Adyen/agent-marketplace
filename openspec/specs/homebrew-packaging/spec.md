# homebrew-packaging Specification

## Purpose
Provide a deterministic Homebrew installation path for stable Agent Marketplace releases on supported macOS and Linux platforms, including authorized access when release assets are private.

## Requirements

### Requirement: Stable releases produce a deterministic platform-specific formula

The formula generator SHALL accept only an exact stable `vMAJOR.MINOR.PATCH` tag, a validated repository owner and name, the four matching local release binaries, and uploaded release metadata containing the same four unique asset names with positive numeric asset IDs. It MUST compute each binary's SHA-256 locally and produce identical formula bytes for identical inputs without making network requests. The formula's Homebrew version metadata SHALL use the release version without the leading `v`. The installed binary MUST report the exact stable tag, including the leading `v`.

#### Scenario: Formula selects each supported release binary
- **WHEN** a valid stable release is supplied for macOS or Linux on arm64 or amd64
- **THEN** the formula selects that platform's exact release asset and its locally computed SHA-256
- **AND** it MUST NOT substitute a different asset for an unsupported platform

#### Scenario: Formula installs the declared Agent Marketplace version
- **WHEN** Homebrew installs a valid generated formula
- **THEN** the selected binary is installed as `bin/agent-marketplace`
- **AND** formula metadata uses the version without `v`, the formula declares Apache-2.0 and depends on Git, and its test asserts that `agent-marketplace --version` equals the exact stable tag including `v`

### Requirement: Invalid release inputs fail before formula output

The generator SHALL reject malformed or non-stable tags, invalid repository coordinates, missing or unreadable platform binaries, and release metadata that is missing, mismatched, duplicated, or has nonpositive asset IDs. A rejected input MUST NOT leave a partial formula or other generated artifact.

#### Scenario: Invalid release data produces no formula
- **WHEN** any required tag, repository coordinate, local binary, or uploaded asset metadata is invalid or incomplete
- **THEN** generation fails with a visible error
- **AND** no formula output is created or changed

### Requirement: Private release downloads use credentials only at fetch time

For a private repository, the formula SHALL address each immutable numeric release asset through GitHub's asset API and request binary content. A missing `HOMEBREW_AGENT_MARKETPLACE_TOKEN` MUST be reported before an asset request is made. Credentials MUST NOT appear in generated formula source, formula metadata, or diagnostics. For a public repository, the formula SHALL use canonical release download URLs and remain installable without a token.

#### Scenario: Private formula defers authentication until asset fetch
- **WHEN** the private formula is generated or evaluated without `HOMEBREW_AGENT_MARKETPLACE_TOKEN`
- **THEN** no credential is required or embedded in the formula or its metadata
- **AND** an attempted asset fetch without a token fails clearly before the request
- **AND** a fetch with a token requests the immutable asset as binary content without exposing the token in output

#### Scenario: Public formula downloads anonymously
- **WHEN** a release is available from a public repository and no token is configured
- **THEN** Homebrew downloads its selected asset from the canonical release URL
- **AND** installation does not require private-repository authentication

### Requirement: Formula publication follows successful stable releases only

The Homebrew publication workflow SHALL run only for an exact stable release classified as `vMAJOR.MINOR.PATCH`, after the existing release job succeeds, including its binary upload. It MUST retain the existing binary assets and release gates unchanged.

#### Scenario: Successful stable release publishes after binary upload
- **WHEN** the stable release classifier accepts the tag and the existing release job completes successfully
- **THEN** the Homebrew publication job runs after that job and publishes the formula for the uploaded release assets
- **AND** the existing four raw binary assets and release gates remain unchanged

#### Scenario: Non-stable or failed release does not publish
- **WHEN** the tag is not an exact stable version or the existing release job fails
- **THEN** the Homebrew publication job does not update the tap branch

### Requirement: Tap publication preserves branch state and limits writes

Publication SHALL update only the dedicated `homebrew` branch and the formula, optional formula download-strategy support file, and `VERSION` that it owns. It MUST preserve unrelated files and existing branch history, MUST NOT write to the source branch, and MUST fail visibly without forcing a conflicting branch-ref update.

#### Scenario: Tap branch creation and update preserve unrelated files
- **WHEN** a formula is published to an absent or existing `homebrew` branch
- **THEN** publication initializes the absent branch or extends the existing commit
- **AND** an existing branch retains unrelated tree entries and history
- **AND** only the owned formula, optional strategy support, and version paths are changed

#### Scenario: Stale publication conflict leaves the branch unchanged
- **WHEN** a branch update conflicts with a concurrent ref change or the GitHub API rejects publication
- **THEN** publication fails visibly without a force update
- **AND** no source branch or unrelated tap path is changed

### Requirement: Published stable versions are monotonic and idempotent

Publication SHALL compare stable versions numerically with the tap branch's `VERSION`. An older release MUST NOT replace a newer formula. An identical release whose owned outputs already match MUST be a no-op. Malformed version state MUST fail closed without updating the branch.

#### Scenario: Older release cannot replace a newer formula
- **WHEN** the tap branch contains a newer valid stable version than the release being published
- **THEN** publication leaves the branch and all its contents unchanged

#### Scenario: Identical release is idempotent
- **WHEN** the tap branch already contains the same stable version and identical generated owned outputs
- **THEN** publication creates no commit and leaves the branch unchanged

#### Scenario: Same-version output conflict fails closed
- **WHEN** the tap branch has the same valid version but its owned outputs differ from the generated release outputs
- **THEN** publication fails visibly without rewriting the branch

#### Scenario: Malformed tap version fails closed
- **WHEN** the existing `VERSION` or required tap metadata cannot be parsed as valid publication state
- **THEN** publication fails visibly without updating the branch
