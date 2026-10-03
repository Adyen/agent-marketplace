## ADDED Requirements

### Requirement: Direct conversion primary reads use captured source authority

Agent Marketplace direct conversion autodetection and primary ingestion MUST read below one captured source root through descriptor-bound regular singly-linked nonblocking stable snapshots with captured-size streaming/growth checks. Typed source boundary refusal MUST stop conversion before staging/publication, MUST NOT become ordinary partial-result materialization, and MUST preserve existing converted output plus original/external bytes and identity/mode/mtime. Explicit flag validation, source/output overlap and generation guards, contained authored file aliases and normal malformed-syntax partial conversion behavior MUST remain supported. No new absolute input/tree/work ceiling or whole-source atomic snapshot is introduced.

#### Scenario: Direct conversion refuses foreign source payloads

- **WHEN** a direct native conversion source primary aliases a foreign or multiply linked unsafe payload
- **THEN** explicit and autodetected conversion MUST retain typed refusal before output mutation, preserving source/external/installed generation, while valid contained-file aliases convert normally
