## ADDED Requirements

### Requirement: Portable virtual source hashes retain captured payload authority

Agent Marketplace MUST authorize skill support payloads used by read-only portable source hashing against one captured source root. Enumeration and regular singly-linked payload reads MUST be descriptor-relative, preserve one unchanged opened generation and apply captured-size streaming/growth checks without introducing a new absolute size/work budget. Contained relative file aliases and executable permissions MUST remain usable, while foreign/unsafe aliases and payload shapes MUST refuse with materialize.write.error before their bytes contribute to an ownership digest. Read-only checking MUST create no output/cache/manifest state and MUST preserve source/external bytes, identity, permissions and modification time.

#### Scenario: Read-only portable source hash refuses foreign aliases

- **WHEN** read-only file-mode materialization encounters a skill support alias outside its captured source root
- **THEN** checking MUST refuse with materialize.write.error, preserve the source alias and external bytes/metadata, and create no output/cache/manifest state

#### Scenario: Contained portable source hashes retain native ownership

- **WHEN** a file-mode skill installed with a contained relative executable support alias is subsequently checked read-only
- **THEN** the virtual source digest MUST retain the installed native content/mode and ownership meaning, report in sync and preserve the manifest identity/mode/mtime
