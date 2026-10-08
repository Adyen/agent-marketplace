## MODIFIED Requirements

### Requirement: SDK quarantined-payload removal retains admitted payload authority

SDK MUST revalidate the discovery-admitted quarantined payload generation immediately before deletion, using captured parent authority and the existing bounded no-follow/nonblocking single-link reader. Changed or unknown payloads observed by final verification MUST return the existing stale-prune conflict, remain untouched by deletion/attempt cleanup and MUST NOT be restored as the admitted original stale file. Genuine stale pruning MUST remain functional. Verification and name-based deletion are separate operations, not an atomic identity-conditional deletion. Deliberate interference by another process running with the same OS user ID and write access to the quarantine directory during that final verification/deletion window is outside the supported threat model; private naming and repeated verification MUST NOT be represented as atomic hostile same-user exclusion or guaranteed preservation in that window. This narrow exclusion does not relax other generation, containment, ownership, restoration, cleanup/recovery or removal-reporting requirements. This rule does not independently fence the earlier conflict-restoration path or redefine removal reporting.

#### Scenario: SDK quarantined-payload replacement preserves removal generation

- **WHEN** public SDK sync verifies a genuinely quarantined stale payload and that name is replaced after initial verification but before final verification with independently authored foreign regular bytes
- **THEN** sync MUST stale-conflict and preserve foreign and retained original bytes/inodes/modes/mtimes, avoid unauthorized stale-name restoration, preserve output membership and clean only owned staging, and restoring valid authority MUST permit actual stale deletion while earlier transaction-prefixed foreign replacement stays unchanged
