## ADDED Requirements

### Requirement: SDK live removal reporting reflects actual deletion receipts

SDK live Removed entries MUST be backed by successful validated stale-payload unlink outcomes, not discovery/planning, conflict refusal, restored replacement, or candidate disappearance before mutation. Physical removal receipts MUST remain independent of later cleanup/close error. Public removal API, original conflict/error identities and complete sorted StaleCandidates plan MUST remain compatible. Dry-run Removed MUST retain prospective plan semantics without filesystem mutation.

#### Scenario: SDK removal receipts distinguish refusal absence and dry-run

- **WHEN** public SDK sync discovers sorted safe A and unsafe B and A is genuinely pruned while B is replaced/refused or disappears before rename
- **THEN** live Removed MUST contain only actual A while the complete A+B candidate plan remains, conflict/original/foreign bytes and metadata/output members/staging guarantees remain intact, dry-run MUST prospectively report A+B without mutation, and healthy controls MUST perform the exact deletion(s) their receipts claim
