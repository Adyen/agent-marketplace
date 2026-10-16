## MODIFIED Requirements

### Requirement: Publication is private journaled and generation-checked

New sensitive aggregates, staging, and mandatory raw-byte safety backups SHALL have private permissions. Library conversion SHALL emit the OpenCode MCP configuration artifact with private permissions and SHALL leave its publication to synchronization's reconciliation. Existing shared-file modes SHALL survive compilation, fragment merging, and native publication, and incompatible requested modes SHALL refuse. Aggregate read/derive/recovery/publication SHALL be serialized per canonical destination and generation-checked with journaled recovery. Mandatory backups SHALL remain enabled even when optional backups are disabled. Identical desired output SHALL cause no rewrite or chmod.

#### Scenario: Creation and conversion retain private modes

- **WHEN** synchronization creates sensitive OpenCode MCP output under a permissive process umask, or library conversion emits it
- **THEN** output, the emitted artifact, and mandatory safety backups SHALL remain owner-readable only throughout publication
- **AND** updating an existing file SHALL preserve its mode and repeating unchanged output SHALL preserve bytes and modification time

#### Scenario: Concurrent or interrupted updates preserve an authorized generation

- **WHEN** two processes update one native aggregate or a process stops during journaled publication
- **THEN** updates SHALL serialize or refuse with `ownership.generation-conflict` rather than mix aggregate and ownership generations
- **AND** recovery SHALL retain handwritten content and refuse unrelated foreign changes
