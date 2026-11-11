## MODIFIED Requirements

### Requirement: Reasoning preference conversion

A recognized reasoning preference SHALL be emitted in the destination
surface's native encoding: a separate effort field when the surface declares
one, or a model-encoded representation when the catalog defines one.

For same-harness conversion with a separate effort field, a known model
available in the destination catalog SHALL be checked against its native
reasoning encodings without rewriting the native model string. Unknown
destination models SHALL retain their native preferences. A resolved family
identity SHALL use its pinned model's capabilities without changing its spelling.

#### Scenario: Supported reasoning emits native encoding

- **WHEN** a resolved model and a supported reasoning level are compiled
- **THEN** the artifact MUST carry the catalog's native effort field value or
  the model-encoded spelling for that level

#### Scenario: Unsupported reasoning warns without suppressing

- **WHEN** the requested reasoning level has no representation for the selected
  destination model or surface
- **THEN** compilation MUST emit the resource with only the reasoning
  preference omitted and report a warning diagnostic

#### Scenario: Conflicting reasoning preference warns and resolves once

- **WHEN** a source model string encodes a reasoning preference and an explicit
  reasoning field disagrees with it
- **THEN** compilation MUST keep the explicit preference, emit a single
  consistent output, and report a warning diagnostic

#### Scenario: Same-harness known models omit unsupported native reasoning

- **WHEN** a subagent is converted to its source layout's harness target and
  its destination catalog model cannot represent the separate reasoning value
- **THEN** conversion MUST preserve the native model spelling and resource body
- **AND** it MUST omit the reasoning field and report the established
  source/target-specific reasoning-omission warning

#### Scenario: Same-harness supported native effort values remain unchanged

- **WHEN** a same-harness subagent's separate reasoning value matches a native
  encoding in the destination catalog, even if its canonical reasoning name
  differs
- **THEN** conversion MUST preserve the native model spelling and reasoning
  value without a model-reasoning loss warning

#### Scenario: Same-harness unknown destination models retain native reasoning

- **WHEN** a same-harness subagent's model identity is absent from the
  destination catalog, even if another harness target knows that identity
- **THEN** conversion MUST preserve the native model and reasoning value
  without a model-reasoning loss warning

#### Scenario: Same-harness family selectors use pinned reasoning capabilities

- **WHEN** a same-harness model selection has a resolved family identity with
  a destination catalog pin
- **THEN** its separate reasoning value MUST be checked against the pinned
  model's native encodings
- **AND** the authored selector MUST remain unchanged
