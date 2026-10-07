## ADDED Requirements

### Requirement: Codex backup namespace retains captured parent generation

CreateCodexSet MUST prepare missing set-directory segments relative to the nearest admitted existing directory descriptor, verify that selected named parent's generation before mutation and retain the prepared authority through existing partition/archive/manifest publication and owned cleanup. Late selected HOME/data/backups/target aliases or real-directory replacements MUST generation-refuse with empty operation and no directory/archive/manifest residue or mutation of original/foreign/native bytes/inodes/modes/mtimes/membership/aliases. Cold/warm creation, supported initial external aliases, private archive and manifest modes and valid empty two-partition set behavior MUST remain. Descriptor Close failures MUST retain causes. No new global budget, content/durability policy, atomic uncooperative-writer exclusion or all-directory rollback is established.

#### Scenario: Codex backup namespace refuses retargeted parent

- **WHEN** public CreateCodexSet with actual content in both native partitions retargets its admitted closest existing HOME/data/backups/target parent to a foreign alias or real directory before preparation
- **THEN** creation MUST generation-refuse preserving all original/foreign/native state without namespace/archive/manifest residue, and restoring original authority MUST publish a genuine complete set whose real RestoreCodexSet recovers both partitions while initial aliases and empty sets remain supported
