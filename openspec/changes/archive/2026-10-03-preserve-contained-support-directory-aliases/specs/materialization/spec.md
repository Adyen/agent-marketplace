## ADDED Requirements

### Requirement: Skill support directory aliases remain scope-contained

Agent Marketplace MUST accept skill support directory aliases and alias chains whose resolution stays within the captured source root. Native installation and read-only portable checking MUST expand the same logical support tree, preserve regular singly-linked payload bytes and executable permissions, and exclude VCS internals at every level. Read-only checking MUST preserve output and source state, and unchanged file-mode output MUST remain a no-op. Escaping, dangling, cyclic, or unsafe-payload aliases MUST refuse with materialize.write.error without replacing an installed generation or importing external bytes. An alias that leaves the source root before returning to it MUST remain refused.

#### Scenario: Contained support directory aliases retain native install and check

- **WHEN** a skill with contained directory aliases, an alias chain, executable file aliases, and nested VCS internals is installed in either materialization mode and checked read-only
- **THEN** its native support files MUST be regular executable snapshots at each logical alias path, VCS internals MUST be absent, checking MUST report in sync without writes, and unchanged file-mode repetition MUST preserve native identity and modification time

#### Scenario: Unsafe support directory aliases preserve the installed generation

- **WHEN** an installed contained support directory alias becomes escaping, dangling, cyclic, leaves and returns to the root, or exposes multiply-linked payloads
- **THEN** read-only checking and native installation MUST report materialize.write.error while preserving installed and external bytes, identity, permissions, and modification time
