# Spec Delta

## ADDED Requirements

### Requirement: Place publication keeps payload and prerequisite identity separate

Place publication SHALL bind the canonical prerequisite graph separately from payload and archive identity. A changed graph for identical payload MUST be publishable as distinct immutable Place evidence while retaining the existing archive digest and previously published graph.

#### Scenario: Identical Place payload publishes with a changed prerequisite graph

- **WHEN** identical payload bytes are published first with an empty prerequisite graph and then with a non-empty graph
- **THEN** both publications MUST succeed with the same payload archive digest, and the earlier graph MUST remain unchanged

### Requirement: Place inspection manifests are immutable at the existing inspection address

At the existing coordinate and profile byte-digest inspection address, ingestion with a different inspection manifest MUST refuse atomically and leave the previous manifest and visible publication unchanged.

#### Scenario: SourceID-only change preserves the original inspection manifest

- **WHEN** a SourceID-only change alters the manifest at the existing coordinate and profile byte-digest inspection address
- **THEN** publication MUST refuse atomically, and a fresh client using the old lock MUST still retrieve the original manifest
