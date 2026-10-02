## ADDED Requirements

### Requirement: Marker alias payload reads retain scoped authority

Agent Marketplace MUST admit marker-merge JSON aliases before reading payload bytes, for mutating and read-only operations. An effective target outside repository/HOME scope, the admitted machine output directory and the trusted cache root MUST be refused without copying foreign bytes into output or changing the alias or target. Accepted targets MUST be regular and singly linked, and their data and permissions MUST describe one unchanged captured generation. Dangling, cyclic, special or multiply linked targets MUST refuse. Contained aliases and legacy cache migrations MAY detach into regular native output while preserving handwritten content and original target state. Refusal MUST report `materialize.write.error`. This contract does not add an absolute input-size or aggregate-work budget.

#### Scenario: Marker merge refuses an external alias before reading

- **WHEN** a repository marker aggregate aliases a foreign regular JSON file outside admitted scope and cache
- **THEN** materialization MUST refuse before importing its payload and preserve the alias and original target bytes and metadata

#### Scenario: Contained marker aliases preserve native content and no-ops

- **WHEN** an accepted contained marker alias contributes handwritten JSON to a desired native MCP aggregate
- **THEN** the desired and handwritten entries and existing permissions MUST be preserved in a regular output, the original target MUST remain unchanged, and repeat sync MUST preserve output identity and modification time

#### Scenario: Read-only marker merge refuses an external alias

- **WHEN** read-only materialization encounters an external marker aggregate alias
- **THEN** checking MUST refuse without reading its payload or changing the alias
