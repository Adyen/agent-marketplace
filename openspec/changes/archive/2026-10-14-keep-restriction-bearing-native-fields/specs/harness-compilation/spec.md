## ADDED Requirements

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
