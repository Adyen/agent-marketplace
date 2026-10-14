# harness-compilation Specification

## Purpose
Defines faithful native serialization of canonical agent resources, keeping scalar metadata separate from explicit typed execution authority.

## Requirements

### Requirement: Unicode YAML separators remain scalar data

Harness Compilation MUST preserve U+0085, U+2028 and U+2029 in YAML frontmatter scalar strings without allowing them to create extra native metadata keys or execution authority. Native strict YAML parsing MUST recover the exact scalar input. Ordinary scalar formatting and explicit typed native sidecars MUST retain their existing behavior. This requirement adds no conversion policy or processing budget.

#### Scenario: Unicode line separators cannot create workflow authority

- **WHEN** a command description contains a YAML Unicode separator followed by inert workflow-metadata text
- **THEN** public Windsurf compilation MUST produce frontmatter whose strict YAML parse retains the exact description without adding an auto_execution_mode field

#### Scenario: Explicit typed workflow authority remains native

- **WHEN** a command's typed Windsurf sidecar declares manual execution with an ordinary description
- **THEN** public compilation MUST retain the ordinary description and explicit manual execution metadata in valid native YAML

### Requirement: Integer metadata remains representable

Harness ingestion MUST reject integer metadata outside the supported native integer range and MUST reject non-finite or fractional values instead of wrapping or truncating them. Representable integral values MUST retain their exact value.

#### Scenario: Integer metadata does not wrap

- **WHEN** canonical, Factory, or Gemini subagent metadata supplies a nesting limit that is out of range, non-finite, or fractional
- **THEN** ingestion MUST reject the subagent rather than accepting a wrapped integer
- **AND** a representable integral nesting limit MUST retain its value

### Requirement: Ambiguous Windsurf identities and rule metadata are refused

Harness Compilation MUST NOT choose a winner between two resources or two metadata values that claim the same identity. When two Windsurf workflows or two Windsurf rules in the same workspace root resolve to the same name, ingestion MUST ingest none of them and MUST report the error `ingest.windsurf.duplicate-identity`; other resources MUST still be ingested. A rule whose frontmatter declares the same key twice MUST NOT be ingested.

#### Scenario: Windsurf workflows with the same name are refused

- **WHEN** two Windsurf workflow files in one workspace declare the same name next to an unrelated workflow
- **THEN** conversion MUST report the error `ingest.windsurf.duplicate-identity`, MUST emit no command for that name, and MUST still emit the unrelated workflow

#### Scenario: Rule frontmatter with a repeated key is refused

- **WHEN** a Cursor rule's frontmatter declares `alwaysApply` twice
- **THEN** conversion MUST emit no rule artifact for it and MUST report an error
