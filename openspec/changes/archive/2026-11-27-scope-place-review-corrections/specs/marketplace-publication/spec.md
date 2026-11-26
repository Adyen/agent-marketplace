## ADDED Requirements

### Requirement: Profile inspection addresses are immutable for one profile digest

At an existing resource coordinate and profile byte-digest inspection address, a SourceID-only change that would produce a different inspection manifest MUST refuse atomically. A fresh client using the old lock MUST continue to retrieve the original manifest. Reassessment with the same manifest and publication at a new profile byte digest remain permitted.

#### Scenario: SourceID-only change preserves the original inspection manifest

- **GIVEN** a profile has been published at a resource coordinate and profile byte-digest inspection address, and a client has locked its original manifest
- **WHEN** a publication changes only the SourceID at that same address and would produce a different inspection manifest
- **THEN** publication MUST refuse atomically without changing the visible registry head, assessment, or inspection manifest
- **AND** a fresh client using the old lock MUST retrieve the original manifest and profile bytes
- **AND** reassessment with the same manifest and publication at a new profile byte digest MUST remain permitted
