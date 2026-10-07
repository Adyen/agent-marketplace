## ADDED Requirements

### Requirement: Ordinary backup namespace preparation retains captured parent authority

Ordinary and mapped Create MUST prepare missing backup directories relative to the nearest admitted existing directory descriptor, verify that selected parent's named generation before mutation, and retain prepared parent authority through existing private archive publication. Late selected parent aliases or real-directory replacements MUST generation-refuse without creating directories/archives or changing original/foreign/native bytes/inodes/modes/mtimes/membership/aliases. Cold and warm creation, supported initial external HOME/data/backups/target aliases, private archive modes, missing mapped-only source roots and empty-source no-op MUST remain. Existing target-alias listing skip policy MUST not change. Descriptor Close failures MUST propagate causes. This adds no arbitrary budget, content/durability policy, atomic uncooperative-writer exclusion or all-directory rollback.

#### Scenario: Ordinary backup namespace refuses retargeted parent

- **WHEN** public ordinary or mapped-only Create retargets an admitted closest existing HOME/data/backups/target parent to a foreign alias or real directory before namespace preparation
- **THEN** creation MUST generation-refuse preserving all original/foreign/native state without directory/archive residue, and restoring original authority MUST create a real private archive whose Restore recovers original bytes while initial aliases and empty-source no-op remain valid
