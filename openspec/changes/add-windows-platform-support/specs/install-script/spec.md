## MODIFIED Requirements

### Requirement: Stable releases publish binary checksums

Every stable release SHALL publish a `SHA256SUMS` asset next to the platform binaries: the four macOS and Linux binaries and the two Windows binaries. The CLI release build's `SHA256SUMS` MUST contain exactly one SHA-256 entry per platform binary, in `sha256sum` format with the bare asset file name, and nothing else; GUI packaging appends its own lines as defined by `gui-distribution`. The four macOS and Linux binary assets and their names MUST remain unchanged, the Windows binary assets MUST be named `agent-marketplace-<tag>-windows-<arch>.exe`, and the checksum asset name MUST NOT begin with `agent-marketplace-`.

#### Scenario: Release build writes checksums for the four binaries
- **WHEN** the release build produces the four macOS and Linux platform binaries for a stable tag
- **THEN** it also produces `SHA256SUMS` listing each of the four binary file names with its SHA-256
- **AND** `sha256sum --check SHA256SUMS` succeeds in the release output directory

#### Scenario: Release build writes checksums for the Windows binaries
- **WHEN** the CLI release build produces the Windows `amd64` and `arm64` binaries for a stable tag
- **THEN** its `SHA256SUMS` also lists `agent-marketplace-<tag>-windows-amd64.exe` and `agent-marketplace-<tag>-windows-arm64.exe` with their SHA-256
- **AND** it lists exactly six entries
