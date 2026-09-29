## Purpose

Defines reachability-based cache collection from local configuration and verified acquisition evidence without requiring network access to unrelated remote sources.

## ADDED Requirements

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
