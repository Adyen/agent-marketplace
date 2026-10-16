# harness-compilation Specification

## Purpose
Defines faithful native serialization and ingestion of canonical agent resources, keeping scalar metadata and unmodeled fields separate from explicit typed execution authority.

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

### Requirement: Converted command skills keep their restrictions

Harness Compilation MUST NOT discard an authority-bearing control while converting between a Devin converted command skill and a command. When the Devin source layout marks a skill with `x-agent-marketplace-kind: command` and the skill declares a `permissions` policy, an `agent` or `subagent` execution selector, or triggers that do not include `user`, ingestion MUST refuse the resource with the error diagnostic `ingest.devin.command-security-unsupported`, and the refused resource MUST produce no artifact on any harness target. A marked skill whose activation includes `user` and whose `allowed-tools` restriction is representable MUST still be ingested as a command; activation other than user-only MUST be narrowed to user-only with the warning `ingest.devin.command-field-dropped`. Devin compilation of a command MUST NOT emit `agent` or `subagent` from command extension fields; it MUST report `compile.canonical-field-overrides-extra` and keep the mapped `allowed-tools` restriction. Ordinary Devin skills MUST keep their native execution selectors.

#### Scenario: Marked Devin skill with controls the command cannot hold is refused

- **WHEN** a Devin source layout skill is marked as a converted command skill and declares a permissions deny policy, model-only activation, or an agent or subagent selector, next to an unmarked sibling skill
- **THEN** conversion to Devin and to Claude MUST report the error `ingest.devin.command-security-unsupported`, MUST emit no artifact for the marked resource, and MUST still emit the sibling skill

#### Scenario: Activation that includes user narrows to a converted command

- **WHEN** a Devin source layout skill is marked as a converted command skill and declares user and model triggers
- **THEN** conversion from Devin to Devin MUST report the warning `ingest.devin.command-field-dropped` and no error, and MUST emit the converted command skill with user-only triggers

#### Scenario: Plain converted command skill still round-trips

- **WHEN** a Devin converted command skill declares user-only activation and a `read` allowed-tools restriction
- **THEN** conversion from Devin to Devin MUST report no error and MUST emit the converted command skill with the command marker, user-only triggers and the `read` restriction

#### Scenario: Command extension fields cannot select a Devin execution agent

- **WHEN** a command carries an `allowed-tools` extension field together with `agent` and `subagent` extension fields
- **THEN** Devin compilation MUST emit the mapped `allowed-tools` restriction without an `agent` or `subagent` field and MUST report `compile.canonical-field-overrides-extra`

#### Scenario: Native Devin skills keep execution selectors

- **WHEN** an unmarked Devin skill declares `agent` and `subagent` execution selectors
- **THEN** conversion from Devin to Devin MUST report no error and MUST emit both selectors unchanged

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
