## ADDED Requirements

### Requirement: Place package acquisition requires indexed prerequisites

Online and cache-only Place package acquisition MUST require a known prerequisite array before reading or publishing package archive bytes. An explicit empty array is known; absent or `null` prerequisites are unknown and MUST refuse without writing cache data.

#### Scenario: Online package acquisition requires indexed prerequisites

- **WHEN** an otherwise eligible exact Place revision has absent or `null` prerequisites
- **THEN** acquisition MUST refuse before requesting its archive or writing package cache data

#### Scenario: Cache-only unknown prerequisites refuse before archive reads

- **WHEN** a locked Place package has missing or `null` cached prerequisite metadata
- **THEN** cache-only acquisition MUST refuse with `marketplace.cache.place-prerequisites-missing` before reading its archive
- **AND** it MUST instruct the user to re-fetch online

#### Scenario: Explicit indexed-empty prerequisites allow offline acquisition

- **WHEN** a locked Place package has cached `requires: []` and its other lock-bound evidence is valid
- **THEN** cache-only acquisition MUST accept the known-empty declaration and verify the cached archive
