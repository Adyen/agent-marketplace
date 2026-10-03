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
