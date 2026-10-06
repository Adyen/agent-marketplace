## ADDED Requirements

### Requirement: SDK quarantined-payload removal retains admitted payload authority

SDK MUST revalidate the discovery-admitted quarantined payload generation at final deletion, using captured parent authority and the existing bounded no-follow/nonblocking single-link reader. Changed or unknown payloads MUST return the existing stale-prune conflict, remain untouched by deletion/attempt cleanup and MUST NOT be restored as the admitted original stale file. Genuine stale pruning MUST remain functional. This rule does not independently fence the earlier conflict-restoration path or redefine removal reporting.

#### Scenario: SDK quarantined-payload replacement preserves removal generation

- **WHEN** public SDK sync verifies a genuinely quarantined stale payload and that name is replaced before deletion with independently authored foreign regular bytes
- **THEN** sync MUST stale-conflict and preserve foreign and retained original bytes/inodes/modes/mtimes, avoid unauthorized stale-name restoration, preserve output membership and clean only owned staging, and restoring valid authority MUST permit actual stale deletion while earlier transaction-prefixed foreign replacement stays unchanged
