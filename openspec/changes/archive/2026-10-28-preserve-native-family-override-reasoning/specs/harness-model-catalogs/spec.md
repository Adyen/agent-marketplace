## ADDED Requirements

### Requirement: Native family overrides preserve reasoning capabilities

On a surface with a separate effort field, an explicit model override
selecting a declared destination catalog family SHALL preserve supported
native reasoning and the authored override selector. Unsupported native
reasoning SHALL be omitted with the established warning without suppressing
the resource.

#### Scenario: Supported native reasoning survives a family override

- **WHEN** an explicit model override selects a declared destination family
  on a separate-effort surface and the requested native reasoning is supported
- **THEN** compilation MUST preserve the override selector, reasoning value,
  and resource body without a model-reasoning loss warning

#### Scenario: Unsupported native reasoning is omitted for a family override

- **WHEN** an explicit model override selects a declared destination family
  on a separate-effort surface and the requested native reasoning is unsupported
- **THEN** compilation MUST preserve the override selector and resource body
- **AND** it MUST omit the reasoning field and emit the established
  source/target-specific reasoning-omission warning
