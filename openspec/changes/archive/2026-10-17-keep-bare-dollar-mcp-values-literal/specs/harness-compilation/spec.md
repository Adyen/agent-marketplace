## ADDED Requirements

### Requirement: Bare dollar names in MCP values stay literal

Canonical MCP server values MUST treat only `${NAME}` and `${NAME:-default}` as environment references; a bare `$NAME` MUST be literal text, as Claude Code expands it. Windsurf compilation MUST emit a bare `$NAME` unchanged. Gemini expands a bare `$NAME` in settings and extension MCP values, so Gemini ingestion MUST canonicalize every bare `$NAME` in an MCP server's string values to `${NAME}`, and Gemini compilation MUST refuse an MCP server whose emitted string values contain a bare `$NAME`, reporting the error `mcp.gemini.interpolation-unrepresentable`, because Gemini documents no escape for the literal.

#### Scenario: Windsurf keeps a bare dollar name literal

- **WHEN** a canonical MCP server with header value `$NOT_A_REF`, and a native Codex MCP server with header value `$SYNTHETIC_KEY`, are converted to Windsurf
- **THEN** each server MUST be emitted with the header value unchanged and no `${env:...}` reference

#### Scenario: Gemini bare references become canonical references

- **WHEN** a Gemini MCP server with environment value `$API_TOKEN` is converted to Windsurf, Claude and Gemini
- **THEN** Windsurf MUST emit `${env:API_TOKEN}`, Claude MUST emit `${API_TOKEN}`, and Gemini MUST emit `${API_TOKEN}`

#### Scenario: Gemini refuses a literal it would expand

- **WHEN** a canonical MCP server with header value `$NOT_A_REF`, or a native Codex MCP server with header value `$SYNTHETIC_KEY`, is converted to Gemini
- **THEN** no Gemini MCP server entry MUST be emitted, and compilation MUST report the error `mcp.gemini.interpolation-unrepresentable`
