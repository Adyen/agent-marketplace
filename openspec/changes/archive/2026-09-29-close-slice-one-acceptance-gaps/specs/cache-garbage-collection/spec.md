## ADDED Requirements

### Requirement: Empty organization Marketplace cache is collectable

Cache garbage collection SHALL succeed without creating an organization acquisition lock or contacting the Marketplace when the configured organization Marketplace has no cached artifacts.

#### Scenario: Organization Marketplace with empty cache

- **WHEN** collection runs for a configured organization Marketplace without an acquisition lock and without cached artifacts
- **THEN** it MUST report no removals, make no network requests, and leave the organization acquisition lock absent

### Requirement: Trusted organization acquisition evidence retains exact references

Cache garbage collection SHALL honor exact artifact references in a trusted organization acquisition lock without rewriting it, while collecting artifacts that no active source references.

#### Scenario: Trusted organization lock retains exact archive

- **WHEN** collection runs with a trusted organization acquisition lock that references one cached archive and a genuine unreferenced archive is cached
- **THEN** it MUST retain the referenced archive, remove the unreferenced archive, make no network requests, and leave the acquisition lock byte-identical
