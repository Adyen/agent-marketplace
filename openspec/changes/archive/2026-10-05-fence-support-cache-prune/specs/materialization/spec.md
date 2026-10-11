## ADDED Requirements

### Requirement: Skill support pruning remains within its captured cache namespace

Agent Marketplace MUST capture the skill-cache directory before discovering support-prune members and MUST perform discovery and removals through that captured directory. The named cache MUST still denote the admitted object before discovery and each removal. Retarget MUST refuse with materialize.write.error before deleting foreign members or installing the refused native link. Compiled artifacts and their required directories MUST remain preserved. Fully managed support reconciliation and initially supported selected root aliases remain compatible. This requirement does not make cache compilation, support copying and native publication a whole-tree transaction.

#### Scenario: Support cache prune refuses retargeted generation

- **WHEN** public native sync replaces the cache with a foreign alias at the support-prune boundary after compiled cache creation
- **THEN** pruning MUST refuse without changing foreign bytes or inode/mode/mtime, publishing the native link or leaving attempt support staging, and restoring the original cache MUST permit real support installation with its mode and deletion of obsolete support
