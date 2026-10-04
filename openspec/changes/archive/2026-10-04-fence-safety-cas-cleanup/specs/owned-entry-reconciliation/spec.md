## ADDED Requirements

### Requirement: CAS safety cleanup retains actual producer authority

Agent Marketplace CAS refusal cleanup MUST retain private actual safety writer parent/payload/published-sidecar generations in the in-memory journal without JSON changes, validate both members before mutation and remove only those observed generations descriptor-relatively. Replacement/unsafe members MUST preserve both replacement and paired evidence while returning the original CAS refusal through the existing cleanup warning boundary. Empty-directory cleanup MUST compare original object and use kernel empty-only removal, with nonempty a harmless no-op. Healthy cleanup MUST still remove its actual attempt. Restart recovery retains safety evidence and MUST NOT fabricate this private authority from BackupPath. No new budget, atomic cleanup transaction or uncooperative-writer exclusion.

#### Scenario: CAS cleanup preserves replaced safety backup

- **WHEN** actual Apply's produced safety payload is replaced after staging and late policy refuses publication
- **THEN** cleanup MUST preserve replacement/original/paired evidence and live prior bytes while stage/journal cleanup occurs, and a healthy late refusal MUST really remove its own backup/sidecar/empty directory/stage/journal
