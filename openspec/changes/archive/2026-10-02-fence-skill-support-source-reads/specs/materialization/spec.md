## ADDED Requirements

### Requirement: Skill support payload reads stay within captured source authority

Agent Marketplace MUST enumerate skill cache support sources through captured directory handles and authorize each opened source payload against its captured source root before reading. Payloads MUST be regular singly-linked snapshots with unchanged identity and size, supplying bytes and permissions from the same generation. Contained relative file aliases MUST remain usable. Escaping aliases and unsafe payload shapes MUST refuse without importing external bytes. Applicable captured-size growth checks do not introduce a new absolute source or aggregate-work budget.

#### Scenario: Support copy refuses a source link outside its root

- **WHEN** a native skill source support file is an alias outside the authorized source root
- **THEN** cache preparation MUST report its stable write diagnostic before native skill publication and preserve external bytes and identity/mode/mtime

#### Scenario: Support copy keeps contained relative alias content and mode

- **WHEN** a native skill source has a relative file alias to regular executable support data within its captured root
- **THEN** actual native installation and repeat sync MUST retain intended support bytes/mode and leave the original source unchanged

#### Scenario: Support source refusal preserves an installed cache generation

- **WHEN** an installed native skill update changes compiled content but its support source refuses admission
- **THEN** cache preparation MUST preserve installed compiled/support bytes and publish no refused support payload, cleaning only attempt-owned staging
