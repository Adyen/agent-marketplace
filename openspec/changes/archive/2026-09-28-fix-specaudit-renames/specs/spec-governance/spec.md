## ADDED Requirements

### Requirement: Requirement renames preserve traceability

The traceability check SHALL apply RENAMED requirement mappings before other operations in each active delta, regardless of section order. Renaming a requirement SHALL preserve its scenario identities; MODIFIED and REMOVED operations SHALL address its new name.

#### Scenario: Renamed requirement is modified

- **WHEN** an active delta renames a requirement and modifies it under its new name while retaining its scenarios
- **THEN** existing scenario annotations MUST remain valid without duplicate-slug findings
- **AND** this MUST hold even when the MODIFIED section precedes RENAMED

#### Scenario: Renamed requirement is removed

- **WHEN** an active delta renames a requirement and removes it under its new name
- **THEN** its scenarios MUST no longer require annotations

#### Scenario: Rename preserves scenario identities

- **WHEN** an active delta only renames a requirement
- **THEN** its existing scenario annotations MUST remain valid
