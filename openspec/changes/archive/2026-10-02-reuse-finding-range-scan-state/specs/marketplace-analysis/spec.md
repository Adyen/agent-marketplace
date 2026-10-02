## Purpose

Defines deterministic marketplace content findings and source-location fidelity while preserving existing detection and processing policies.

## ADDED Requirements

### Requirement: Ordered text finding ranges retain rune-location fidelity

Marketplace Analysis MUST retain detection identity, finding count and ordering, source file and analyzer, and one-based rune line/column locations when avoiding repeated prefix conversion for ordered findings. Reused range-conversion state MUST be invocation-local, and invalid standalone offsets MUST retain a file-only range. This requirement adds no input, finding or graph-work budget and does not alter detection rules.

#### Scenario: Dense comment findings retain ordered rune locations

- **WHEN** bounded inert text contains an ordered HTML comment on each line after a non-ASCII rune
- **THEN** public analysis MUST emit one HTML-comment finding per line in original order, at column two with unchanged file/analyzer identity
