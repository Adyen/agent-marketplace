# harness-compilation Specification

## Purpose
Defines faithful native serialization of canonical agent resources, keeping scalar metadata, target sidecars and extension fields from granting authority beyond the explicit canonical resource.

## Requirements

### Requirement: Unicode YAML separators remain scalar data

Harness Compilation MUST preserve U+0085, U+2028 and U+2029 in YAML frontmatter scalar strings without allowing them to create extra native metadata keys or execution authority. Native strict YAML parsing MUST recover the exact scalar input. Ordinary scalar formatting and explicit typed native sidecars MUST retain their existing behavior. This requirement adds no conversion policy or processing budget.

#### Scenario: Unicode line separators cannot create workflow authority

- **WHEN** a command description contains a YAML Unicode separator followed by inert workflow-metadata text
- **THEN** public Windsurf compilation MUST produce frontmatter whose strict YAML parse retains the exact description without adding an auto_execution_mode field

#### Scenario: Explicit typed workflow authority remains native

- **WHEN** a command's typed Windsurf sidecar declares manual execution with an ordinary description
- **THEN** public compilation MUST retain the ordinary description and explicit manual execution metadata in valid native YAML

### Requirement: Target sidecars cannot widen canonical authority

Harness Compilation MUST NOT let a target sidecar or extension field grant more authority than the canonical resource. A Claude or Cursor subagent tool sidecar MUST only restate or narrow the canonical tool list: every sidecar entry MUST be a canonical entry or that entry's target mapping, otherwise compilation MUST emit no subagent and MUST report the error `subagent.security-restriction-unsupported`. A Claude or Cursor hook-type sidecar MUST equal the canonical hook type, otherwise compilation MUST emit no hook and MUST report the error `hook.claude.unsupported-type` or `hook.cursor.unsupported-type`. Codex compilation MUST emit a native MCP sidecar only when it matches the canonical server, otherwise it MUST emit no server and MUST report the error `mcp.codex.security-unsupported`. Claude, Cursor, Factory and Gemini MCP compilation MUST NOT emit a modeled MCP connection field (`command`, `args`, `env`, `url`, `transport`, `headers`, `oauthClientId`, `oauthClientSecret` or `oauthResource`) from extension fields, including case variants, and MUST report `compile.canonical-field-overrides-extra`.

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
