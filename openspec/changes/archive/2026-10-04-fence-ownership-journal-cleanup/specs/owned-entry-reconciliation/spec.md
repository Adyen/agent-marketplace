## ADDED Requirements

### Requirement: Journal cleanup removes only the observed journal generation

Agent Marketplace Apply, CAS refusal and recovery journal cleanup MUST retain private captured parent and actual opened or published-producer journal identities without changing serialized JSON. Cleanup MUST revalidate the observed parent object and full file state before descriptor-relative removal. Replaced or disappeared observed journals MUST refuse rather than deleting replacements or reporting successful absence. Shared metadata read/write paths MUST preserve strict canonical parent verification and genuine optional read absence. Failure-injection semantics remain supported after authority validation. Record publication leases, staged/safety cleanup and namespace evidence remain separate; no whole-mutation transaction or uncooperative-writer exclusion is introduced.

#### Scenario: Journal cleanup preserves replacement generation

- **WHEN** actual Apply replaces its journal with foreign inert bytes after actual state commit
- **THEN** cleanup MUST refuse typed generation identity and preserve replacement/original metadata while actual native/state remain committed; restoring the original journal MUST let public recovery report already-committed and really remove it
