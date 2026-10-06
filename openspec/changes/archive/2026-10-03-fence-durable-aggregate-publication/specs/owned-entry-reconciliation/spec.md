## ADDED Requirements

### Requirement: Durable aggregate publication retains captured authority through staging

Agent Marketplace ownership Apply MUST capture authorized aggregate parent and destination generation before journal preparation, then exclusively stage and publish descriptor-relatively under that authority. Retargeted parents MUST NOT receive private staged bytes. Captured destination and completed staged generation MUST be revalidated before durable rename; same-byte replacement generations MUST refuse with ownership generation-conflict identity/code. Shared existing modes and private creation MUST remain supported. Refusal cleanup MUST remove only attempt-owned staged inodes through original captured parents; foreign staged replacements and injected-crash recovery evidence MUST remain intact. Existing journal/crash ordering and durability contract MUST remain supported. Recovery/state/archive mutation remains separate; no arbitrary new limits or exclusion of uncooperative writers is introduced.

#### Scenario: Durable staging refusal preserves authority

- **WHEN** ownership Apply encounters a parent retarget after journal preparation or a same-byte destination/staged replacement after staging
- **THEN** it MUST refuse without foreign private staging or replacement overwrite, preserve native/user bytes and clean only attempt-owned refusal staging while valid shared-mode/crash controls continue working
