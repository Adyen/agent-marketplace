## ADDED Requirements

### Requirement: Abbreviated digests in the TUI detail summary pane

The TUI right-hand detail summary pane SHALL abbreviate algorithm-qualified hex digests to `<algorithm>:<first 12 hex>…` while keeping short non-digest revisions intact. When a Marketplace row's `Revision` equals its `Digest`, the summary pane SHALL omit the duplicate `Revision:` line. The full detail view (`enter`) SHALL retain the complete digest, and verification SHALL remain exact.

#### Scenario: Summary pane abbreviates SHA-256 digests and deduplicates identical revision

- **WHEN** the user selects a Marketplace discovery row whose `Revision` and `ContentDigest` are the same 64-hex `sha256:` digest
- **THEN** the right-hand summary pane MUST show a single abbreviated `Digest:` line with the algorithm and first 12 hex characters followed by `…` and MUST NOT repeat an identical `Revision:` line
- **AND** pressing `enter` to open full detail MUST still display the complete unabbreviated digest

#### Scenario: Distinct non-digest revision stays visible

- **WHEN** a Marketplace row has a human-readable revision such as `v1.2.0` distinct from its `sha256:` content digest
- **THEN** the right-hand summary pane MUST display `Revision: v1.2.0` alongside the abbreviated `Digest:` line
