# harness-compilation Specification

## Purpose
Defines faithful native serialization and ingestion of canonical agent resources, keeping scalar metadata, unmodeled fields, target sidecars and extension fields from granting authority beyond the explicit typed canonical resource.

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

### Requirement: Target sidecars cannot widen canonical authority

Harness Compilation MUST NOT let a target sidecar or extension field grant more authority than the canonical resource. A Claude or Cursor subagent tool sidecar MUST only restate or narrow the canonical tool list: every sidecar entry MUST be a canonical entry or a target tool the canonical list covers, otherwise compilation MUST emit no subagent and MUST report the error `subagent.security-restriction-unsupported`. A Claude or Cursor hook-type sidecar MUST equal the canonical hook type, otherwise compilation MUST emit no hook and MUST report the error `hook.claude.unsupported-type` or `hook.cursor.unsupported-type`. Codex compilation MUST emit a native MCP sidecar only when it matches the canonical server, otherwise it MUST emit no server and MUST report the error `mcp.codex.security-unsupported`. Claude, Cursor, Factory and Gemini MCP compilation MUST NOT emit a modeled MCP connection field (`command`, `args`, `env`, `url`, `transport`, `headers`, `oauthClientId`, `oauthClientSecret` or `oauthResource`) from extension fields, including case variants, and MUST report `compile.canonical-field-overrides-extra`.

#### Scenario: Subagent tool sidecar cannot widen the canonical tool list

- **WHEN** a Marketplace resource artifact payload for a subagent restricts its canonical tools to `Read` and carries Claude and Cursor tool sidecars naming `Bash`
- **THEN** Claude and Cursor compilation MUST emit no subagent and MUST report the error `subagent.security-restriction-unsupported`

#### Scenario: Subagent tool sidecar may narrow the canonical tool list

- **WHEN** a subagent's canonical tools are `Read` and `Grep` and its Claude and Cursor tool sidecars name only `Read`
- **THEN** Claude and Cursor compilation MUST report no error and MUST emit a subagent restricted to `Read`

#### Scenario: Hook type sidecar cannot activate a command

- **WHEN** a Marketplace resource artifact payload for a canonical prompt hook carries a command payload and Claude and Cursor hook-type sidecars set to `command`
- **THEN** Claude and Cursor compilation MUST emit no hook and MUST report the error `hook.claude.unsupported-type` or `hook.cursor.unsupported-type`

#### Scenario: Codex MCP sidecar must match the canonical server

- **WHEN** a Marketplace resource artifact payload for an MCP server declares a canonical command and carries a Codex MCP sidecar with a different command and an extra forwarded environment variable
- **THEN** Codex compilation MUST emit no server and MUST report the error `mcp.codex.security-unsupported`

#### Scenario: Codex-native MCP servers still round-trip

- **WHEN** a Codex source layout declares a stdio MCP server with literal environment values and forwarded environment variables
- **THEN** conversion from Codex to Codex MCP output MUST report no error and MUST emit the same command, arguments, environment values and forwarded variables

#### Scenario: Modeled MCP fields cannot arrive through extension fields

- **WHEN** an MCP server has no typed command and carries `command`, `args` and a case-variant `URL` as extension fields
- **THEN** Claude, Cursor, Factory and Gemini compilation MUST emit none of those fields and MUST report `compile.canonical-field-overrides-extra`

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

### Requirement: Ambiguous Windsurf identities and rule metadata are refused

Harness Compilation MUST NOT choose a winner between two resources or two metadata values that claim the same identity. When two Windsurf workflows or two Windsurf rules in the same workspace root resolve to the same name, ingestion MUST ingest none of them, MUST NOT let a user-root resource with that name take their place, and MUST report the error `ingest.windsurf.duplicate-identity`; other resources MUST still be ingested. A rule whose frontmatter declares the same key twice MUST NOT be ingested.

#### Scenario: Windsurf workflows with the same name are refused

- **WHEN** two Windsurf workflow files in one workspace declare the same name next to an unrelated workflow
- **THEN** conversion MUST report the error `ingest.windsurf.duplicate-identity`, MUST emit no command for that name, and MUST still emit the unrelated workflow

#### Scenario: Rule frontmatter with a repeated key is refused

- **WHEN** a Cursor rule's frontmatter declares `alwaysApply` twice
- **THEN** conversion MUST emit no rule artifact for it and MUST report an error

### Requirement: Restriction-bearing native fields are preserved or refused

Harness Compilation MUST NOT silently drop a native field that restricts or locates what a resource runs. Gemini ingestion MUST keep every member of a subagent's inline MCP server, and Gemini compilation MUST emit the members it did not model, including `includeTools` and `excludeTools`, unchanged; modeled members MUST NOT be overridden by preserved ones. Windsurf ingestion MUST read a hook's documented `working_directory`; Windsurf compilation MUST emit a non-empty value, and compilation of a hook with a non-empty value to any other target MUST emit no hook and MUST report the error `hook.windsurf.native-fields-unsupported`. Devin MCP compilation MUST NOT drop a single argument it cannot represent; it MUST emit no server and MUST report the error `mcp.devin.interpolation-unrepresentable`.

#### Scenario: Gemini inline MCP server restrictions round-trip

- **WHEN** a Gemini subagent's inline MCP server declares `includeTools`, `excludeTools` and `timeout`
- **THEN** conversion from Gemini to Gemini MUST report no error and MUST emit all three members unchanged

#### Scenario: Windsurf hook working directory round-trips

- **WHEN** a Windsurf hook declares a non-empty `working_directory`
- **THEN** conversion from Windsurf to Windsurf MUST report no error and MUST emit the same `working_directory`

#### Scenario: Targets without a hook working directory refuse the hook

- **WHEN** a Windsurf hook declaring a non-empty `working_directory` is converted to Claude
- **THEN** compilation MUST emit no hook and MUST report the error `hook.windsurf.native-fields-unsupported`

#### Scenario: Devin refuses an MCP server whose argument it cannot represent

- **WHEN** an MCP server argument uses a file interpolation Devin has no syntax for
- **THEN** Devin compilation MUST emit no server entry and MUST report the error `mcp.devin.interpolation-unrepresentable`

### Requirement: Foreign skill tool lists never become pre-approvals

Claude, Cursor and Gemini read a skill's `allowed-tools` as a pre-approval: listed tools run without a permission prompt. When a skill's `allowed-tools` list carries another harness's tool provenance, Claude, Cursor and Gemini compilation MUST omit the list and MUST report the warning `skill.allowed-tools-omitted`, and the skill MUST still be emitted. A list without provenance (canonical Claude vocabulary) or from the target's own harness MUST be emitted unchanged.

#### Scenario: Devin and Factory skill tool lists are omitted on pre-approval targets

- **WHEN** a Devin skill with `allowed-tools` `read` and `exec`, and a Factory skill with `allowed-tools` `Read` and `Edit`, are converted to Claude, Cursor and Gemini
- **THEN** each emitted skill MUST have no `allowed-tools` field, and compilation MUST report the warning `skill.allowed-tools-omitted`

#### Scenario: Same-harness and canonical skill tool lists are unchanged

- **WHEN** a Claude skill with `allowed-tools` `Read` is converted to Claude, and a Gemini skill with `allowed-tools` `read_file` is converted to Gemini
- **THEN** each emitted skill MUST keep its `allowed-tools` list unchanged, with no `skill.allowed-tools-omitted` warning
