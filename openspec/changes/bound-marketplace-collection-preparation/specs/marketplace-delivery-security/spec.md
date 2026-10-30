## ADDED Requirements

### Requirement: Nested collection preparation is bounded

Marketplace `install` and `update` preparation MUST fetch, cache and traverse each content-addressed nested collection at most once per preparation. Later include paths MUST reuse its verified result. Cycle detection, revocation enforcement and every per-collection check MUST still apply on its first verification. Preparation MUST apply the lock-bootstrap work limit of 13,981 units and MUST refuse with the resource-limit error before fetching further manifests once traversal exceeds it.

#### Scenario: Collection preparation fetches each nested collection once

- **WHEN** two nested collections in one collection both include the same nested collection
- **THEN** preparation MUST request that shared collection's revision and manifest once each and MUST still verify all three nested collections

#### Scenario: Collection preparation stays within the lock bootstrap limit

- **WHEN** a collection's nested traversal exceeds 13,981 work units
- **THEN** `PrepareLocator` MUST refuse with the resource-limit error
