## ADDED Requirements

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
