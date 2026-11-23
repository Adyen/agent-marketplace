## ADDED Requirements

### Requirement: Status shows selected prerequisite provenance

The status main resource table SHALL show `requires <kind>:<name>` in VIA for selected resources with requirement provenance and SHALL retain collection provenance when present. Disabled-expansion preview resources SHALL NOT be represented as actual synced selections. Requirement reporting SHALL retain existing drift inspection, failed-resource attribution, terminal escaping and read-only behavior.

#### Scenario: Status distinguishes selected relationships from previews

- **WHEN** status inspects a selected prerequisite and collection relationships with expansion enabled, or the same eligible prerequisites are only advisory previews with expansion disabled
- **THEN** selected relationships MUST appear in main-table VIA without losing collection provenance
- **AND** preview-only resources MUST NOT appear as actual synced selections, and status MUST leave configuration, locks and output unchanged
