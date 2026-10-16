# harness-opencode Specification

## Purpose
Defines opt-in OpenCode full harness support, faithful native resource translation, safe source-layout ingestion, and compatibility across Agent Marketplace workflows.

## Requirements

### Requirement: OpenCode is an opt-in harness target and source layout

Agent Marketplace SHALL accept canonical identifier `opencode` for harness target selection, entry-level narrowing, and `source_harness`, and the harness-compilation library SHALL accept it as both the source and the target of conversion. Configuration validation, initialization choices, TUI selection, status, doctor, and backup SHALL expose OpenCode consistently. Existing default harness target lists and existing selections SHALL remain unchanged.

#### Scenario: OpenCode selection reaches consumer workflows

- **WHEN** a user explicitly selects OpenCode through configuration or the TUI
- **THEN** OpenCode SHALL appear in the resolved harness targets and native destination summaries and selected resources SHALL be eligible for installation and backup
- **AND** library conversion SHALL accept `opencode` as both source and target

#### Scenario: Existing configurations retain their harness targets

- **WHEN** an existing configuration is read, synchronized, initialized with existing defaults, or migrated without selecting OpenCode
- **THEN** OpenCode SHALL NOT be added to its harness targets or materialized destinations

### Requirement: OpenCode detection uses a source marker

Autodetection SHALL recognize an OpenCode source marker only when `.opencode/` holds an OpenCode resource location (`skills`, `commands`, `agents`, `plugins`, their singular aliases, or `hooks/agent-marketplace`) or an `opencode.json`/`opencode.jsonc` file; other content such as dependency manifests SHALL NOT be a marker. Autodetection SHALL NOT infer OpenCode from shared skills directories alone. When a source without a plugin manifest or Devin marker would be detected without OpenCode as a layout that carries its own workspace marker (`.claude`, `.codex`, `.factory`, `.cursor`, `.gemini`, or `.windsurf`), that layout SHALL keep precedence over an OpenCode marker. Otherwise an OpenCode marker SHALL win over shared skills locations, except that a source root with no other detected layout that contains bundle subdirectories SHALL remain a container of those bundles. Container discovery SHALL skip `.opencode` as a harness folder. Whenever an OpenCode marker coexists with another detected layout, autodetection SHALL report warning `ingest.autodetect.ambiguous` naming the selected and ignored layouts and recommending an explicit `source_harness`. Explicit source-layout selection SHALL remain available and SHALL NOT report the warning. Supported native plural directories and legacy singular aliases SHALL be ingested with documented deterministic precedence; duplicate identities without an unambiguous native winner SHALL be suppressed with severity-error diagnostic `ingest.opencode.duplicate-identity`.

#### Scenario: OpenCode marker distinguishes a shared skills layout

- **WHEN** a bundle contains an `.opencode` source marker alongside shared `.agents/skills` or bare skills locations
- **THEN** autodetection SHALL identify OpenCode, discover its supported resources, and report `ingest.autodetect.ambiguous`
- **AND** removing all OpenCode-specific evidence SHALL prevent shared skills locations alone from identifying OpenCode

#### Scenario: Existing workspace layouts keep precedence over an OpenCode marker

- **WHEN** a source without a plugin manifest carries Claude, Codex, Factory, Cursor, Gemini, or Windsurf workspace resources and an `.opencode` source marker
- **THEN** autodetection SHALL select the layout it selected before OpenCode was registered and ingest its resources

#### Scenario: A dependency-only OpenCode directory is not a marker

- **WHEN** `.opencode/` contains only dependency manifests or installed dependencies
- **THEN** autodetection SHALL NOT identify OpenCode and SHALL NOT report a coexisting layout

#### Scenario: Coexisting layouts are reported

- **WHEN** autodetection selects a layout while another detected layout coexists with an OpenCode marker
- **THEN** the catalog SHALL carry warning `ingest.autodetect.ambiguous` naming both layouts and recommending `source_harness`
- **AND** an explicit `source_harness` SHALL select that layout without the warning

#### Scenario: Container registries keep their bundles beside an OpenCode folder

- **WHEN** a source root without another detected layout contains bundle subdirectories and an `.opencode` source marker
- **THEN** discovery SHALL read the bundles, not read `.opencode` as a bundle, and report `ingest.autodetect.ambiguous`
- **AND** an explicit `source_harness: opencode` SHALL read the root as OpenCode

#### Scenario: Native aliases do not create duplicate resources

- **WHEN** supported plural and legacy singular directories or native declarations describe the same resource identity
- **THEN** ingestion SHALL apply the documented native precedence or report `ingest.opencode.duplicate-identity` and suppress the ambiguous identity
- **AND** independent valid resources SHALL remain discoverable

### Requirement: OpenCode resources retain their native identity

Compilation and ingestion SHALL support skills, commands, subagents, MCP servers, and the supported declarative hooks described by `opencode-hooks`. Collections SHALL continue to select their member resources without becoming native artifacts. Skills SHALL use `skills/<name>/SKILL.md`, commands SHALL use `commands/<name>.md`, and subagents SHALL use `agents/<name>.md` with native subagent mode. Supported templates, argument placeholders, model/agent selections, tool controls, local/remote MCP forms, enabled state, and authentication fields SHALL preserve their meaning in bidirectional conversion. Non-security metadata loss SHALL produce a stable coded diagnostic. Rules SHALL produce `rule.opencode.unsupported` without writing shared instruction files.

#### Scenario: All selectable resource kinds reach the native layout

- **WHEN** a collection selects a skill, command, subagent, supported hook, and MCP server for OpenCode in repository or machine scope
- **THEN** each member SHALL materialize in its native representation and the collection SHALL produce no separate native resource
- **AND** a command SHALL remain a command and a subagent SHALL retain native subagent mode

#### Scenario: Native fields survive a supported round trip

- **WHEN** supported OpenCode commands, subagents, and MCP servers are ingested and compiled back to OpenCode or converted through another supported source layout
- **THEN** their supported native identity, template/argument meaning, selections, execution restrictions, enabled state, and authentication meaning SHALL be preserved
- **AND** every unrepresentable field SHALL be diagnosed rather than silently dropped

#### Scenario: Rules preserve shared instructions

- **WHEN** a rule is compiled for OpenCode
- **THEN** compilation SHALL report `rule.opencode.unsupported` and emit no rule artifact
- **AND** existing `AGENTS.md` and other shared instruction files SHALL remain unchanged

### Requirement: Translation preserves execution authority

Explicit execution restrictions SHALL retain their effective meaning, including ordered permission rules and explicit empty policies. Unsupported restrictions SHALL suppress the affected resource with a severity-error diagnostic and make the consuming conversion or synchronization exit nonzero. Agent Marketplace SHALL NOT replace restrictions with inert metadata, broaden permissions, change a subagent to a primary agent, or modify consumer-wide permission policy to make a conversion succeed. Unsupported skill restrictions SHALL report `skill.opencode.security-restriction-unsupported`; subagent and MCP restrictions SHALL use `subagent.opencode.restriction-unsupported` and `mcp.opencode.unrepresentable` respectively.

#### Scenario: A restricted skill is not emitted as an unrestricted skill

- **WHEN** a skill declares a restriction that OpenCode skill execution cannot enforce
- **THEN** compilation SHALL report `skill.opencode.security-restriction-unsupported` and suppress the skill
- **AND** an unrestricted valid sibling SHALL still compile

#### Scenario: Ordered and empty agent policies retain their meaning

- **WHEN** a supported subagent declares ordered overlapping permission rules or an explicit empty execution policy
- **THEN** native execution SHALL retain that policy's effective restrictions and subagent role
- **AND** any unrepresentable policy SHALL suppress the subagent without granting broader authority

### Requirement: Native parsing has one bounded interpretation

OpenCode JSON/JSONC and frontmatter ingestion SHALL reject duplicate keys, conflicting aliases of modeled fields, malformed typed values, and untyped extras that supply reserved execution fields, including nested and explicitly empty fields. Unsupported YAML merge fields and inline config command/agent definitions SHALL report an error; an inline identity SHALL suppress the Markdown resource it overrides. Comment-only frontmatter SHALL remain valid. Arbitrary resource and environment dictionary keys SHALL remain case-sensitive. Unicode separators in scalar metadata SHALL remain scalar data. Applicable read limits SHALL cover every admitted loader and opened regular-file read; oversized or growing input SHALL refuse before unsafe output. Rejected input SHALL report severity-error `ingest.opencode.invalid-resource` or `ingest.opencode.read-refused` without including sensitive values.

#### Scenario: Ambiguous execution fields are rejected in both orders

- **WHEN** native input contains exact duplicates or conflicting modeled-field aliases in either order, including nested or empty execution fields
- **THEN** ingestion SHALL report `ingest.opencode.invalid-resource` and suppress the affected resource
- **AND** valid native groups and distinct case-sensitive dictionary keys SHALL retain their intended behavior

#### Scenario: Scalar metadata cannot add execution fields

- **WHEN** native scalar metadata contains U+0085, U+2028, U+2029, quotes, or control characters
- **THEN** supported compilation and strict native parsing SHALL recover the intended scalar without creating execution-authority fields

#### Scenario: Read limits apply at the actual consumer boundary

- **WHEN** an OpenCode input exceeds an applicable read limit or grows beyond its admitted snapshot during reading
- **THEN** the consumer SHALL report a stable error, emit no affected output, and retain previously installed content
- **AND** valid input within the limit SHALL still reach native installation

### Requirement: Literal values and references remain distinct

Ingestion SHALL NOT expand source references against host environment values or read host secret files. Supported literal values and intentional environment/file references SHALL retain their native meaning through the pinned OpenCode loader. If safe representation is unavailable, compilation SHALL suppress the affected resource with a severity-error diagnostic rather than emit syntax that introduces fields, unintended reads, or additional execution authority.

#### Scenario: Literal and reference payloads cannot inject configuration

- **WHEN** MCP values contain literal reference-like text, intentional references, hostile quotes, or synthetic file-reference sentinels
- **THEN** Marketplace ingestion SHALL perform no host reference expansion and native loading of supported output SHALL retain the intended values without extra configuration fields or unintended file reads
- **AND** unrepresentable MCP values SHALL suppress the server with `mcp.opencode.unrepresentable`

### Requirement: Production workflows do not execute source resources

Discovery, ingestion, compilation, conversion, synchronization, read-only inspection, and repair SHALL NOT execute source plugins, commands, hook handlers, or MCP servers, install runtime dependencies, perform authentication, approve trust, or edit `.gitignore`. OpenCode, Bun, Node, and npm SHALL NOT be required for these workflows. Arbitrary source plugins SHALL be diagnosed with `ingest.opencode.plugin-unsupported` without being imported.

#### Scenario: Executable source content remains inert during consumer operations

- **WHEN** discovery, conversion, sync, check, status, or repair encounters synthetic executable plugins, commands, hooks, or MCP definitions
- **THEN** none of their execution sentinels SHALL run and no authentication, trust, dependency, or `.gitignore` state SHALL change
- **AND** valid supported resources SHALL work without an OpenCode executable installed

### Requirement: New resource fields retain inspection and compatibility protections

OpenCode resources and generated declarative hooks SHALL remain subject to Marketplace resource assessment and inspection projection policies. Diagnostics, logs, telemetry, security findings, and repair previews SHALL NOT disclose credentials or sensitive execution payloads. Support for OpenCode SHALL NOT alter existing harness behavior, acquisition-lock bytes, resolver precedence, required-resource policy, or existing source-revision semantics merely because a new harness target is registered. An accepted resource assessment SHALL NOT be presented as safety certification.

#### Scenario: Sensitive native fields are redacted in consumer findings

- **WHEN** an OpenCode resource contains synthetic secrets in native metadata, MCP authentication, or hook declarations and a diagnostic or assessment is produced
- **THEN** the relevant finding SHALL remain attributable without exposing the synthetic secret in diagnostics, logs, telemetry, or repair previews
- **AND** a valid inspection projection SHALL still expose permitted resource content under the existing inspection policy

#### Scenario: Existing locks and harness targets remain compatible

- **WHEN** OpenCode resources are selected alongside an existing harness target using an unchanged acquisition lock
- **THEN** synchronization SHALL retain the exact locked acquisition and existing harness behavior without rewriting healthy lock bytes or weakening required-resource policy

### Requirement: Translation respects supported tool and model mappings

OpenCode translation SHALL resolve command and subagent model selections through the shipped harness model catalogs. A canonical model with no OpenCode catalog identifier SHALL be omitted and reported with warning `model.unresolved` rather than emitted as a known-invalid selection. Native OpenCode model selections SHALL keep their spelling through model provenance and SHALL be resolved by catalog identity for other harness targets. Agent tool restrictions SHALL be translated to OpenCode permission keys: a key SHALL be emitted only when every native tool it governs lies within the requested capability set, and the emitted keys SHALL cover that whole set; otherwise the subagent SHALL be refused with severity-error `subagent.opencode.restriction-unsupported`. Explicit tool mapping overrides SHALL be honored consistently in restrictive subagent policies and supported hook matchers, without granting additional tools.

#### Scenario: An unmappable canonical model is not emitted

- **WHEN** an OpenCode command or subagent selects a canonical model with no OpenCode catalog identifier
- **THEN** translation SHALL report `model.unresolved` and omit the model selection
- **AND** catalog-mapped identifiers, native OpenCode selections, and explicit model overrides SHALL remain intact

#### Scenario: Native model selections keep their provenance

- **WHEN** an OpenCode subagent with a provider-qualified native model is ingested and compiled for OpenCode and for another harness target
- **THEN** the OpenCode output SHALL keep the native model unchanged
- **AND** the other harness target SHALL receive the catalog identifier for the same model identity or report `model.unresolved`

#### Scenario: A tool override applies consistently

- **WHEN** a consumer supplies a supported explicit tool mapping override
- **THEN** supported restrictive subagent policies and hook matchers SHALL use that mapping
- **AND** unsupported mappings SHALL fail closed rather than expand tool authority

#### Scenario: Agent restrictions follow permission granularity

- **WHEN** a subagent allows or denies a tool set that maps onto whole OpenCode permission keys, such as file read, edit, and write together
- **THEN** translation SHALL emit those permission keys with the authored action
- **AND** a set that only part of a permission key covers, such as write without edit, SHALL refuse the subagent with `subagent.opencode.restriction-unsupported` instead of emitting an unenforced or broader rule
