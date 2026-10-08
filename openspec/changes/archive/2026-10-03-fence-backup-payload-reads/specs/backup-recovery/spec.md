## ADDED Requirements

### Requirement: Restore archive payloads require captured regular authority

Agent Marketplace ordinary restore extraction and Codex archive preflight MUST admit archive payloads through captured regular, singly linked, nonblocking opened-file authority before gzip/tar decoding. Final payload aliases and unsafe shapes MUST refuse without granting archive payload authority. Streaming MUST remain bounded at captured compressed-file size and validate opened/named state; valid regular archives MUST still restore actual archived native content. Cleanup errors MUST propagate. These controls do not add arbitrary expanded/compressed budgets, a transactional extraction, output publication authority or exclusion of uncooperative writers.

#### Scenario: Restore payload hardlink refuses before extraction

- **WHEN** a real created archive selected for restore is multiply linked while current installed content differs from the archive
- **THEN** restore MUST refuse with typed unsafe-file identity before extraction and preserve current/foreign bytes, while a valid regular archive MUST actually restore the archived generation
