# harness-model-catalogs Specification

## Purpose
Defines how model and reasoning selections on skills and subagents convert
between harness targets from shipped per-harness catalog data, how source-native
spellings and provenance survive ingestion and archives, and how unrepresentable
selections are diagnosed without suppressing resources.

## Requirements

### Requirement: Catalog-driven model resolution

Compilation SHALL resolve a resource's model selection against the destination
harness target's shipped catalog, using the source layout's harness as
provenance when present. Resolution SHALL emit the native model spelling for a
resolved selection and SHALL NOT silently substitute a different model.

#### Scenario: Unresolved model warns and keeps the resource

- **WHEN** a subagent selects a model the destination catalog cannot resolve
- **THEN** compilation MUST emit the resource without a model field
- **AND** it MUST report a warning diagnostic describing the omission

#### Scenario: Resolved model emits the native spelling

- **WHEN** a selected model resolves in the destination catalog
- **THEN** the emitted artifact MUST carry that catalog's native model spelling

#### Scenario: No cross-provider approximation

- **WHEN** a selected model has no entry in the destination catalog
- **THEN** the compiler MUST NOT approximate it with another provider's model
- **AND** it MUST apply the unresolved-model warning behavior instead

#### Scenario: Floating family selector pinned with notice

- **WHEN** a floating family selector resolves but the destination catalog pins
  it to a concrete identity
- **THEN** compilation MUST emit the pinned identity with an informational
  diagnostic

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

### Requirement: Omission and inheritance semantics

An omitted model SHALL follow the destination surface's declared omission
semantics — parent inheritance or the harness default — and SHALL NOT be
rewritten to a different semantic.

#### Scenario: Omitted model uses the surface's native semantics

- **WHEN** a resource omits the model selection
- **THEN** the destination artifact MUST omit the model field or emit the
  surface's native inheritance sentinel, matching the surface's declared
  omission semantics

#### Scenario: Crossing inheritance and default boundaries warns

- **WHEN** the source's omission semantics (inherit versus default) differ from
  the destination surface's
- **THEN** compilation MUST apply the destination semantics and report a
  warning diagnostic

#### Scenario: Reasoning on inherited selection is omitted when unrepresentable

- **WHEN** a reasoning preference accompanies an inherited selection and the
  destination surface cannot represent effort with inheritance
- **THEN** compilation MUST omit the reasoning preference and report a warning
  diagnostic

### Requirement: Source-native spellings and provenance

Ingestion SHALL retain native model strings verbatim and record the source
layout's harness as model-source provenance; compilation SHALL use provenance
to preserve same-harness selections without requiring a canonical alias.
An absent or JSON `null` `ModelSource` SHALL mean no model-source provenance
is defined and SHALL use legacy canonical resolution, including legacy sidecar
precedence. It SHALL NOT clear an explicit model selection; only an omitted
model selection follows the destination surface's inheritance/default semantics.

#### Scenario: Same-harness native model strings round-trip unchanged

- **WHEN** a resource is ingested from a source layout and compiled back to the
  same harness target
- **THEN** its native model string MUST be preserved verbatim

#### Scenario: Ingestion records model-source provenance

- **WHEN** a model-bearing resource is ingested from a source layout
- **THEN** the retained model string MUST carry that source layout's harness as
  provenance

#### Scenario: Authoritative fields override stale legacy sidecars

- **WHEN** a resource carries model-source provenance and conflicting legacy
  native sidecars
- **THEN** compilation MUST use the current top-level model and effort fields
  and ignore the stale sidecar values

#### Scenario: Extras cannot forge provenance or resurrect dropped fields

- **WHEN** arbitrary extension fields imitate provenance or dropped model
  fields
- **THEN** they MUST NOT establish provenance or reappear in emitted artifacts

#### Scenario: Ambiguous or malformed provenance is rejected

- **WHEN** an archive payload carries duplicate provenance or a non-string,
  non-null provenance value
- **THEN** decoding MUST fail rather than guess a source
- **AND** a single JSON `null` provenance value MUST instead be accepted and
  behave identically to absent provenance, preserving any explicit model selection

#### Scenario: Legacy archives remain byte-stable and resolvable

- **WHEN** a resource without provenance round-trips through the canonical
  archive form
- **THEN** its payload MUST be byte-identical and its model selection MUST
  still resolve

### Requirement: Source-revision digest compatibility

Provenance-bearing resources SHALL retain provenance in the current
source-revision fallback digest while legacy source revisions keep their
historical projection.

#### Scenario: Legacy source revisions ignore provenance

- **WHEN** a provenance-bearing resource is digested under source-revision
  v1, v2, or v3
- **THEN** the content digest MUST equal the historical projected payload
- **AND** the runtime resource MUST NOT be mutated

#### Scenario: Current source revision retains provenance

- **WHEN** a provenance-bearing resource is digested under source-revision v4
- **THEN** the content digest MUST cover the full provenance-bearing resource

### Requirement: Model warnings do not break materialization

Model and reasoning diagnostics SHALL NOT remove resources from
materialization output or cause perpetual drift.

#### Scenario: Model warnings keep materialized resources converged

- **WHEN** a resolved resource carries an unresolvable model
- **THEN** materialization MUST still write the resource without the model
  field, report a warning, and converge on the next check

### Requirement: Shipped catalogs are validated data

Every registered harness target SHALL ship an embedded catalog consumed by the
offline catalog check; malformed catalog data MUST be rejected.

#### Scenario: Every registered harness target ships a catalog

- **WHEN** the catalog check loads shipped catalogs
- **THEN** every registered harness target MUST have one

#### Scenario: Malformed catalogs are rejected

- **WHEN** a catalog carries an unknown key, duplicate mapping, wrong harness,
  invalid surface declaration, malformed evidence, ambiguous encoding, or a
  broken family pin
- **THEN** catalog validation MUST fail

#### Scenario: Conversion fixtures prove catalog mappings

- **WHEN** the offline catalog check runs
- **THEN** each declared conversion fixture MUST produce its expected model
  and reasoning output

### Requirement: Reviewed Codex and Factory model additions

Canonical and cross-harness subagent conversion SHALL resolve
`openai/gpt-6.1-terra` to Codex's native
`gpt-6.1-terra` model with low, medium, high, xhigh, and max reasoning.
Factory SHALL resolve `deepseek/deepseek-v4.1-flash-1017`,
`minimax/minimax-m3.1-flash`, `nvidia/nemotron-3.5-super`,
`openai/gpt-6-luna-fast`, `openai/gpt-6-sol-fast`, and
`openai/gpt-6.1-sol-ultrafast` to their corresponding unqualified native model
IDs. Factory reasoning support SHALL remain limited to the intersection of
each model's documented levels and the custom-droid field's low/medium/high
levels. Unsupported reasoning SHALL retain the selected model and report
`model.reasoning-omitted` or its established source/target-specific
compatibility code.

#### Scenario: Reviewed additions emit native model and reasoning fields

- **WHEN** a canonical or cross-harness subagent selects a reviewed addition for its
  supported destination with a supported reasoning level
- **THEN** conversion SHALL emit that model's native ID and reasoning value

#### Scenario: Priority variants retain their distinct fast identity

- **WHEN** a Devin GPT-6 priority variant is converted to Factory or a matching
  Factory fast model is converted to Devin
- **AND** its reasoning level is explicit and supported by both harnesses
- **THEN** conversion SHALL retain the corresponding fast identity and
  supported reasoning level
- **AND** a destination without that fast identity SHALL report
  `model.unresolved` rather than substitute the base model

#### Scenario: Factory fast model without reasoning is omitted for Devin

- **WHEN** Factory's `gpt-6-sol-fast` is converted to Devin without a reasoning
  level
- **THEN** conversion SHALL omit the model and report
  `subagent.devin.model-unmappable`
- **AND** the subagent resource and body SHALL remain intact

#### Scenario: Reviewed additions omit unsupported reasoning without substitution

- **WHEN** a reviewed model addition is selected with an unsupported reasoning
  level for the destination resource format
- **THEN** conversion SHALL retain the model and omit the reasoning value
- **AND** it SHALL report `model.reasoning-omitted` or its established
  source/target-specific compatibility code without selecting a different
  effort level
