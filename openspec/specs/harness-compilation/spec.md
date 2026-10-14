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

### Requirement: Restriction-bearing native fields are preserved or refused

Harness Compilation MUST NOT silently drop a native field that restricts or locates what a resource runs. Gemini ingestion MUST keep every member of a subagent's inline MCP server, and Gemini compilation MUST emit the members it did not model, including `includeTools` and `excludeTools`, unchanged; modeled members MUST NOT be overridden by preserved ones. Windsurf ingestion MUST read a hook's documented `working_directory`; Windsurf compilation MUST emit it, and compilation to any other target MUST emit no hook and MUST report the error `hook.windsurf.native-fields-unsupported`. Devin MCP compilation MUST NOT drop a single argument it cannot represent; it MUST emit no server and MUST report the error `mcp.devin.interpolation-unrepresentable`.

#### Scenario: Gemini inline MCP server restrictions round-trip

- **WHEN** a Gemini subagent's inline MCP server declares `includeTools`, `excludeTools` and `timeout`
- **THEN** conversion from Gemini to Gemini MUST report no error and MUST emit all three members unchanged

#### Scenario: Windsurf hook working directory round-trips

- **WHEN** a Windsurf hook declares `working_directory`
- **THEN** conversion from Windsurf to Windsurf MUST report no error and MUST emit the same `working_directory`

#### Scenario: Targets without a hook working directory refuse the hook

- **WHEN** a Windsurf hook declaring `working_directory` is converted to Claude
- **THEN** compilation MUST emit no hook and MUST report the error `hook.windsurf.native-fields-unsupported`

#### Scenario: Devin refuses an MCP server whose argument it cannot represent

- **WHEN** an MCP server argument uses a file interpolation Devin has no syntax for
- **THEN** Devin compilation MUST emit no server entry and MUST report the error `mcp.devin.interpolation-unrepresentable`
