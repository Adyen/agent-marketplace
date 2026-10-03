# backup-recovery Specification

## Purpose
Define filesystem authority for backup discovery and recovery so untrusted metadata cannot authorize restore or retention decisions outside admitted backup generations.

## Requirements

### Requirement: Codex backup metadata claims require captured regular manifests

Agent Marketplace Codex backup-set restore, listing and rotation discovery MUST enumerate and read manifests below captured backup-directory authority. Opened manifests MUST remain regular, singly linked and nonblocking at captured size with state/named validation; aliases MUST refuse before metadata claims authorize restore or rotation. Archive-presence metadata queries MUST stay beneath captured directory authority. Genuine optional missing directory and malformed-JSON filtering MAY retain their current semantics; unsafe/read/generation failures MUST NOT supply valid claims. Valid regular manifests MUST still restore the actual archived managed content. Payload/archive mutation consumers remain separate; no arbitrary new processing budget or transaction is introduced.

#### Scenario: Codex manifest alias refuses before restore

- **WHEN** a real complete two-partition backup set is selected through an external manifest alias while current installed content differs from the archive
- **THEN** restore MUST refuse with typed unsafe identity and preserve current/foreign bytes, while the original regular manifest MUST restore the actual archived generation

### Requirement: Restore archive payloads require captured regular authority

Agent Marketplace ordinary restore extraction and Codex archive preflight MUST admit archive payloads through captured regular, singly linked, nonblocking opened-file authority before gzip/tar decoding. Final payload aliases and unsafe shapes MUST refuse without granting archive payload authority. Streaming MUST remain bounded at captured compressed-file size and validate opened/named state; valid regular archives MUST still restore actual archived native content. Cleanup errors MUST propagate. These controls do not add arbitrary expanded/compressed budgets, a transactional extraction, output publication authority or exclusion of uncooperative writers.

#### Scenario: Restore payload hardlink refuses before extraction

- **WHEN** a real created archive selected for restore is multiply linked while current installed content differs from the archive
- **THEN** restore MUST refuse with typed unsafe-file identity before extraction and preserve current/foreign bytes, while a valid regular archive MUST actually restore the archived generation

### Requirement: Regular restore publication retains captured output authority

Agent Marketplace ordinary regular-file and explicitly mapped native-file restore MUST retain captured authorized output parent, strict regular/single-link destination and completed staged identity through publication. Archive bytes MUST stage privately below captured descriptors rather than truncate live destinations. Opened/named target, parent, staged state and admitted absence MUST be revalidated before descriptor-relative rename. Refusal MUST preserve foreign/current/replacement bytes and remove only attempt-owned staged inodes. Valid ordinary archive-mode and mapped permission-mode semantics MUST remain supported without adding per-file fsync, arbitrary new budgets, all-output extraction transaction or exclusion of uncooperative writers. Directory/symlink/source-archive/evidence mutations remain separate.

#### Scenario: Restore file hardlink refuses without foreign write

- **WHEN** a real restore selects a multiply linked regular destination while archive and current user bytes differ
- **THEN** it MUST refuse with typed unsafe-file identity without changing current/foreign bytes, while the regular destination MUST still restore actual archived content and mode

### Requirement: Restore tree namespace mutation stays under captured roots

Agent Marketplace directory and symbolic-leaf extraction MUST verify the named root generation against its captured authority before mutation. Directory preparation and symbolic-leaf parent traversal MUST stay below strict captured descriptors; symbolic namespace removal/link creation MUST NOT reopen an absolute output pathname. Existing entry path and symbolic-target policies MUST remain supported. This namespace control does not claim generation-atomic recursive member deletion, atomic symbolic replacement, all-output transactional extraction or exclusion of uncooperative writers, and adds no processing budget or durability sync.

#### Scenario: Restore tree retarget refuses before foreign mutation

- **WHEN** the extraction root is retargeted while compressed input is consumed after root capture for a directory or symbolic entry
- **THEN** extraction MUST refuse with typed generation identity before creating foreign entries, while valid input MUST really restore the selected directory or symbolic leaf
