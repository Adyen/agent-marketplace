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

### Requirement: Incomplete Marketplace metadata writes do not block collection

Garbage collection SHALL ignore incomplete writer-owned metadata entries under an unlocked Marketplace endpoint, while retaining valid live artifacts and collecting unreferenced artifacts. Malformed completed revision metadata MUST prevent deletion.

#### Scenario: Interrupted metadata write

- **WHEN** an unlocked endpoint has a cached live artifact, an unreferenced artifact, and an incomplete temporary file or empty intermediate metadata directory
- **THEN** collection MUST retain the live artifact, remove the unreferenced artifact, and leave the incomplete entries untouched

#### Scenario: Malformed completed revision

- **WHEN** a completed revision metadata file for an unlocked endpoint is malformed
- **THEN** collection MUST fail without deleting artifacts

### Requirement: Marketplace metadata reads are bounded

Garbage collection MUST reject oversized completed revision metadata before deleting artifacts and MUST NOT modify files outside the cache.

#### Scenario: Oversized revision metadata

- **WHEN** an unlocked endpoint's completed revision metadata exceeds the supported size
- **THEN** collection MUST return a bounded error without deleting artifacts or changing external files

### Requirement: Collection uses current reachable inputs

Garbage collection SHALL use current local reachability at collection time; dry-run and collection on unchanged inputs SHALL identify the same candidates.

#### Scenario: Dry run then collect

- **WHEN** a dry run and a real collection use unchanged configuration, locks, and cache metadata
- **THEN** they MUST report the same candidates and the dry run MUST delete nothing

#### Scenario: Configuration or lock changes while waiting for lease

- **WHEN** configuration or a deployed acquisition lock changes while collection waits for its lease
- **THEN** collection MUST either use the changed inputs or report a generation conflict before deleting newly referenced artifacts

### Requirement: Collection preflight does not authorize stale policy

Real cache collection SHALL revalidate the local configuration and deployed acquisition evidence used to determine reachability after preflight and before deletion. A changed or newly deployed input SHALL produce a generation conflict without deleting cache objects or acquiring resources over the network.

#### Scenario: Organization acquisition lock appears after preflight

- **WHEN** an organization acquisition lock is deployed after collection preflight used its absence
- **THEN** collection SHALL report a generation conflict, delete no cache objects, and make no network requests

#### Scenario: Configuration changes after preflight

- **WHEN** configuration changes after collection preflight computed reachable objects
- **THEN** collection SHALL report a generation conflict and delete no cache objects
