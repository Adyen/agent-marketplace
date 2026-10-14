## ADDED Requirements

### Requirement: Required selection misses stay hard across layers

When several configured entries for the same source location (URL, ref, and path) select a resource that the location does not provide, the miss SHALL be reported once and SHALL be required whenever any of those entries is required, regardless of layer order. A required miss SHALL remain an error rather than a warning in `sync`, `status`, and the TUI.

#### Scenario: Optional entry ordered before a required entry for the same missing resource

- **WHEN** an earlier configuration layer selects a missing resource from a source location without `required`, and a later layer selects the same resource from the same location with `required: true`
- **THEN** exactly one not-found diagnostic MUST be reported for the resource and it MUST be required
- **AND** the TUI MUST NOT assess drift as ready, MUST show the row as a hard unavailable row, and a reload of that snapshot MUST fail
