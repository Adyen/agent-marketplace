## ADDED Requirements

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
