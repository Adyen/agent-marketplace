## ADDED Requirements

### Requirement: Collection expansion stays within the work limit

Resolving selections MUST NOT perform more than 13,981 units of collection expansion work, the lock-bootstrap limit. Each expanded collection costs one unit, plus one per member and one per include, counted once per include path. Exceeding the limit MUST be a hard resolution error, so sync refuses before materializing or pruning. Shared nested collections, branch-local cycle detection, ordering and diagnostics MUST be unchanged within the limit. The limit has no override.

#### Scenario: Collection expansion stays within the work limit

- **WHEN** a selected collection's shared nested DAG expands beyond the work limit
- **THEN** resolution MUST report the collection-expansion error, and a shared DAG within the limit MUST still expand its members
