## ADDED Requirements

### Requirement: Top-level collection lookup preserves required selection

A top-level collection request MUST NOT disable automatic resource selection from a required registry that has no explicitly authored resource or tag selection. Collection lookup SHALL still find the requested collection in any configured source. An explicitly authored required subset SHALL retain its existing selection semantics.

#### Scenario: Top-level collection keeps required auto-selection

- **GIVEN** an organization registry is required and automatically selects its catalog
- **WHEN** repository configuration includes a top-level collection available in an organization or repository source
- **THEN** all automatically selected organization resources MUST remain selected and materialized in machine scope
- **AND** the requested collection MUST resolve without a spurious missing-collection diagnostic

#### Scenario: Top-level collection preserves authored required subset

- **GIVEN** a required registry explicitly selects a subset of its catalog
- **WHEN** repository configuration includes a top-level collection
- **THEN** the explicitly selected required resources and collection members MUST remain selected
- **AND** unrelated catalog resources MUST NOT become selected solely because this lookup occurs
