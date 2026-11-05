## ADDED Requirements

### Requirement: Declared Claude aggregate inspection failures are visible

When cleanup of relocated Claude Code output inspects a declared aggregate,
only a missing path or a path below a non-directory component SHALL be treated
as absence. Other inspection failures MUST produce an error diagnostic naming
the aggregate, rather than successful synchronization. The failure SHALL NOT
prevent cleanup of safe sibling aggregates.

#### Scenario: Declared aggregate inspection errors remain visible

- **WHEN** a declared aggregate cannot be inspected because its component contains a directory-link cycle
- **THEN** cleanup MUST report a file-scoped error and MUST still prune a safe sibling's stale owned entry
- **AND** a genuinely missing component or a regular file occupying the component path MUST retain the normal absence behavior
