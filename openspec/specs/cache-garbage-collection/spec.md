# cache-garbage-collection Specification

## Purpose
Defines reachability-based cache collection from local configuration and verified acquisition evidence without requiring network access to unrelated remote sources.

## Requirements

### Requirement: Missing organization lock does not stop collection

Cache garbage collection SHALL retain reachable objects and collect unreachable objects when an organization Marketplace configuration intentionally has no acquisition lock; it MUST NOT create that lock as a side effect.

#### Scenario: Organization Marketplace without a lock

- **WHEN** garbage collection runs with a v4 organization Marketplace configuration and no deployed organization acquisition lock
- **THEN** it MUST collect unreachable cache objects while retaining reachable ones and leave the organization lock absent

### Requirement: Invalid deployed evidence blocks deletion

Garbage collection MUST NOT remove cache objects when deployed acquisition evidence needed to determine reachability is malformed or mismatched.

#### Scenario: Mismatched organization lock

- **WHEN** the deployed organization acquisition lock conflicts with the organization configuration
- **THEN** garbage collection MUST fail without deleting cache objects

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
