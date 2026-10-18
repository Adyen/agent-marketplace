## ADDED Requirements

### Requirement: Canonical hook metadata normalization is resource-scoped

Normalization of the canonical hook serializer's null `Extra` scaffolding MUST
remain hook-specific. Decoding other resource kinds MUST preserve their existing
unknown metadata and native output.

#### Scenario: Non-hook canonical metadata compatibility

- **GIVEN** a canonical skill, command, subagent or MCP server contains `Extra: null`
- **WHEN** the resource is decoded for native compilation
- **THEN** its historical unknown metadata MUST remain preserved
- **AND** hook-specific metadata normalization MUST NOT change the native output
