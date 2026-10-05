## ADDED Requirements

### Requirement: SDK stage creation and writes retain captured directory authority

SDK Materialize MUST create output directories below captured selected ancestry and retain admitted output root before private stage creation. Named-root replacement MUST refuse before creating foreign stage members. Private staging MUST start with directory mode0700, match its original admitted directory object, and prepare/write files descriptor-relatively; existing-mode chmod MUST use the opened file. Existing umask/file modes, stage-all-artifacts, dry-run and partial-result semantics remain compatible. Refusal MUST preserve foreign bytes/metadata/membership and retain prior own-inode cleanup. This requirement does not establish individual staged/destination file-generation leases or stale discovery/removal.

#### Scenario: SDK stage creation refuses replaced output root

- **WHEN** public SDK Materialize replaces admitted output root immediately before stage creation
- **THEN** it MUST refuse generation conflict without creating foreign stage members or changing foreign bytes/inode/mode/mtime, leave original root without attempt members, and healthy nested source-mode publication MUST really write bytes and clean its stage
