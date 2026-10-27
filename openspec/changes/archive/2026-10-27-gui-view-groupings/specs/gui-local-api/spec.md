## ADDED Requirements

### Requirement: Snapshot row source identity

Each snapshot row SHALL carry a source identity and a display label. The identity SHALL distinguish normalized registry URL, ref, and normalized configured registry subpath, and SHALL be `local` for a row without a registry URL. The label SHALL be the registry display name or URL-derived repository name with the full normalized subpath, or `(local)` without registry metadata, and SHALL be sanitized of terminal control data.

#### Scenario: Rows from distinct sources carry distinct identities

- **WHEN** the snapshot contains rows from sources that share a registry name but differ in ref or subpath, a row without a registry URL, and a registry name containing control characters
- **THEN** rows from different refs or subpaths SHALL carry different identities with the shared name and subpath as labels, the local row SHALL carry identity `local` and label `(local)`, and no label SHALL contain control characters
