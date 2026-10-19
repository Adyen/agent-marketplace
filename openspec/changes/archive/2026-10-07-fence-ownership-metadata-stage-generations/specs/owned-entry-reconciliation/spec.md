## ADDED Requirements

### Requirement: Ownership metadata publication retains produced staging authority

Ownership state, journal and safety-manifest publication MUST retain produced staged identity through final parent/original-generation and staged-reader validation before rename. A replacement stage MUST generation-refuse without overwriting original destination metadata/absence or deleting the replacement. Refusal MUST preserve original/foreign bytes/inodes/modes/mtimes, valid durable recovery evidence and established native publication phases. Healthy recovery and retries MUST genuinely commit matching state/journal generations and raw-byte safety manifests with existing private modes. Post-rename pin mismatch MUST NOT substitute for pre-action validation. No new processing budget, content/durability policy, whole transaction rollback, complete snapshot or atomic uncooperative-writer exclusion is established.

#### Scenario: Metadata publication retains produced staged generation

- **WHEN** actual Apply or BackupOnly replaces the produced metadata stage before state, prepared-journal, journal-phase or safety-manifest publication
- **THEN** publication MUST generation-refuse preserving original destination/absence, both staging generations and recovery evidence, and genuine recovery/retry MUST commit the intended ownership generation or produce the real native safety bytes and matching manifest without owned residue
