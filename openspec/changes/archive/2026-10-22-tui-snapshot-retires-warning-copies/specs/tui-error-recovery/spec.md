## ADDED Requirements

### Requirement: Operation copies of snapshot warnings follow the snapshot

The TUI SHALL count and list a warning once when an operation repeats a warning that the current snapshot already reports, such as a missing optional selection that a sync reports again. When an accepted snapshot no longer reports a warning that the previous snapshot reported, the operation copies of that warning SHALL be retired with it. Operation warnings that no snapshot reported SHALL persist until a later operation of the same kind supersedes them.

#### Scenario: Operation copies of snapshot warnings follow the snapshot

- **WHEN** a snapshot reports a missing optional selection, a sync reports the same warning, and the user later removes the selection and reloads
- **THEN** the footer MUST show `⚠ 1 warnings` after the sync, not two
- **AND** after the reload no warning about the removed selection MUST remain
