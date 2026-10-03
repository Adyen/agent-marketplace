## ADDED Requirements

### Requirement: Source revision payload hashes use captured read authority

Agent Marketplace MUST hash primary and intrinsic support payloads below a captured revision source root using descriptor-bound regular singly-linked nonblocking snapshots with captured-size streaming/growth and stable-state checks. Payload boundary refusals, including typed ingestion refusals, MUST fail revision creation as hard coded/typed errors rather than canonical-primary fallback or omitted-resource warnings, and MUST produce no partial manifest payload. Existing compatibility schemas and bytes, canonical collection serialization, configured-root checks, established link policy and genuine optional absence behavior MUST remain intact. No new absolute input/tree/work ceiling or whole-mutable-source atomic snapshot is introduced.

#### Scenario: Source revision hashing refuses post-ingest unsafe payloads

- **WHEN** an admitted primary or intrinsic support payload becomes multiply linked before actual source-revision hashing
- **THEN** Build MUST fail with the typed unsafe-file identity, produce no partial revision payload and preserve original source bytes/identity/mode/mtime, while valid deterministic/compatibility builds continue
