## ADDED Requirements

### Requirement: Ownership locks admit safe opened metadata leaves

Agent Marketplace ownership lock acquisition MUST open final leaves without following aliases or blocking on special files, and validate opened regular/single-link shape before kernel acquisition, after a wait succeeds and before diagnostic truncation. Initial unsafe shape MUST refuse with typed filesystem cause and no held session. Late unsafe metadata shape MUST preserve bytes at the existing best-effort diagnostic boundary while kernel lock/release semantics remain intact. Preserve timeout, cancellation, reentrancy, unsupported-kernel fallback and supported selected state/shared locks-directory alias exclusion. Directory namespace pinning remains separate; no new budget, transaction or uncooperative-writer exclusion.

#### Scenario: Lock admission refuses hardlinked metadata

- **WHEN** public LockContext sees a lock leaf hardlinked to inert foreign bytes
- **THEN** acquisition MUST refuse before metadata writes and preserve foreign bytes/session absence, while a real regular leaf MUST acquire, write the current pid diagnostic and release normally
