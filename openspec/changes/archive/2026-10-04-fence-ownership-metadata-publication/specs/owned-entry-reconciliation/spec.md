## ADDED Requirements

### Requirement: State and phase-journal publication stays under captured record authority

Agent Marketplace state and phase-journal publication MUST capture strict existing canonical state/ownership/key parent authority before private staging, retain exclusive600 staged producer identity, validate produced opened/named generations and parent before durable descriptor-relative publication, and clean only tracked attempt-created staged inodes beneath captured directories. Commit MUST NOT recreate a missing record parent. Public prepared-directory/JSON/crash semantics and private600 metadata MUST remain unchanged. This namespace publisher does not retain a prior loaded metadata lease across an entire mutation; prior-generation binding, namespace preparation, safety metadata and journal/recovery cleanup remain separate. No new budget, whole-mutation transaction or uncooperative-writer exclusion is introduced.

#### Scenario: Retargeted record parent cannot receive metadata publication

- **WHEN** public Apply reaches after-aggregate-rename and its record parent is retargeted to foreign state/journal files
- **THEN** metadata publication MUST refuse typed unsafe/conflict before foreign metadata/staging writes, preserving both foreign payloads and the admitted prepared journal while the already-published aggregate remains next; valid Apply MUST still commit readable private metadata without staging residue
