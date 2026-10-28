## ADDED Requirements

### Requirement: Unused non-directory components do not block cleanup

Materialization SHALL ignore an existing non-directory at a harness component
location when that component has no desired artifacts. It MUST NOT block cleanup
of independent output. A non-directory collision with desired component output
MUST still refuse without replacing the existing file.

#### Scenario: Unused non-directory components do not block cleanup

- **WHEN** an unused harness component is a regular file and another component contains stale managed output
- **THEN** synchronization MUST preserve the file's bytes and metadata and clean the independent stale output
- **AND** desired output requiring the non-directory component MUST still refuse without replacing that file
