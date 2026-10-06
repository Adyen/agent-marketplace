## ADDED Requirements

### Requirement: SDK quarantine publication and reservation cleanup retain creator authority

SDK stale pruning MUST retain the descriptor identity of each exclusively created private quarantine reservation, require original regular singly linked empty reservation before rename, and remove only matching attempt-created reservations during refusal cleanup. Unknown or replacement authority MUST be preserved with the existing stale-prune conflict; meaningful reader/close failures MUST not become success. Existing regular pruning and restoration behavior MUST remain functional. This requirement does not independently fence later quarantined-payload deletion/restoration, removal reporting or empty-parent cleanup.

#### Scenario: SDK quarantine reservation replacement preserves foreign payload

- **WHEN** public SDK sync replaces its actual private quarantine reservation before rename with an independently authored foreign regular payload
- **THEN** it MUST stale-conflict without consuming/deleting foreign bytes/inode/mode/mtime, preserve original retained empty reservation and selected stale bytes/output membership, clean only owned staging, and a healthy retry MUST actually prune selected stale payload while preserving the earlier transaction-prefixed replacement
