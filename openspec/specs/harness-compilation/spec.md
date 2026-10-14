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

### Requirement: Project and plugin roots stay distinct

Harness Compilation MUST NOT turn a project-directory reference into a plugin-root reference, and MUST translate plugin-root tokens between harnesses that name them differently. Gemini ingestion MUST map only `${extensionPath}` to the canonical plugin root `${CLAUDE_PLUGIN_ROOT}`; `${GEMINI_PROJECT_DIR}` and `${CLAUDE_PROJECT_DIR}` in MCP server values, command prompts and subagent bodies MUST be preserved as written. Factory MCP ingestion MUST map `${DROID_PLUGIN_ROOT}` to `${CLAUDE_PLUGIN_ROOT}`, and Factory MCP compilation MUST map `${CLAUDE_PLUGIN_ROOT}` to `${DROID_PLUGIN_ROOT}`.

#### Scenario: Gemini project directory never becomes the plugin root

- **WHEN** a Gemini MCP server's arguments reference `${GEMINI_PROJECT_DIR}` and `${extensionPath}`
- **THEN** conversion from Gemini to Gemini MUST preserve both tokens, conversion to Claude MUST map only `${extensionPath}` to `${CLAUDE_PLUGIN_ROOT}`, and conversion to Devin with a configured literal plugin root MUST substitute that root only for the extension reference

#### Scenario: Factory plugin root round-trips through MCP servers

- **WHEN** a Factory MCP server's command, arguments and environment reference `${DROID_PLUGIN_ROOT}`
- **THEN** conversion from Factory to Factory MUST emit `${DROID_PLUGIN_ROOT}` in every value, and conversion to Claude MUST emit `${CLAUDE_PLUGIN_ROOT}` in every value

#### Scenario: Canonical plugin root compiles to the Factory token

- **WHEN** a Claude MCP server's command, arguments and environment reference `${CLAUDE_PLUGIN_ROOT}`
- **THEN** conversion from Claude to Factory MUST emit `${DROID_PLUGIN_ROOT}` in every value
