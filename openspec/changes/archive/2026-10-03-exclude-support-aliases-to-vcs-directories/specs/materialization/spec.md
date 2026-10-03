## ADDED Requirements

### Requirement: Skill support directory aliases respect Git exclusions

Agent Marketplace MUST exclude support directory aliases whose contained resolved directory target is `.git` or a descendant of `.git`, even when the logical alias has another name. Real support copying and read-only portable checking MUST omit the same logical subtree while retaining ordinary contained directory aliases and executable file snapshots.

#### Scenario: Support directory aliases cannot expose VCS internals

- **WHEN** a skill has ordinary contained support aliases and aliases targeting `.git` and a nested `.git` subdirectory under different logical names
- **THEN** native installation in either materialization mode MUST retain ordinary executable support snapshots but omit both excluded alias subtrees, and read-only checking MUST report in sync without writes
