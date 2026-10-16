## ADDED Requirements

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
