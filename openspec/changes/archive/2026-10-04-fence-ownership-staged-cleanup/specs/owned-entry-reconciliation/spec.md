## ADDED Requirements

### Requirement: Recovery staged cleanup requires aggregate-scoped recorded payload authority

Agent Marketplace CAS and recovery staged cleanup MUST reject arbitrary metadata-nominated paths: staged paths MUST be absolute, share aggregate parent and match the aggregate's generation-specific staged prefix beneath canonical root authority. Captured opened staged files MUST remain regular, singly linked and nonblocking; bytes read at captured size MUST match the recorded desired digest before removal. Revalidate named parent and use descriptor-relative generation-checked removal. Preserve empty collision-disarmed paths and genuine validated stage absence, current journal format and valid crash recovery. Restart cleanup uses recorded digest plus opened generation, not persisted inode ownership; identical-byte replacements after process death are not distinguished. Safety cleanup/prior record leases remain separate; no new budget, transaction or uncooperative-writer exclusion is added.

#### Scenario: Recovery staged cleanup refuses foreign path authority

- **WHEN** public Recover reads a real prepared-crash journal modified to nominate an inert outside-root staged file
- **THEN** it MUST refuse typed unsafe-path/no outcomes and preserve foreign/live/stage/evidence while restoring the true staged path MUST allow actual rollback and stage/journal removal
