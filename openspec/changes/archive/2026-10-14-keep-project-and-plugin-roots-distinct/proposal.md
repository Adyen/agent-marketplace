## Why

Plugin-root and project-directory tokens select different files. Gemini ingestion rewrote `${GEMINI_PROJECT_DIR}` and `${CLAUDE_PROJECT_DIR}` in MCP servers, command prompts and subagent bodies to the plugin-root token. Every target then resolves a project path inside the plugin: Gemini emits `${extensionPath}`, Claude `${CLAUDE_PLUGIN_ROOT}`, and targets that substitute a literal plugin root point the server at the installed registry copy instead of the project. Hooks already keep the two roots apart through native provenance; MCP and prompt values did not.

Factory MCP servers also kept the wrong harness's token in both directions: Factory output carried `${CLAUDE_PLUGIN_ROOT}`, and Factory input carried `${DROID_PLUGIN_ROOT}` into other targets.

## What Changes

- Gemini ingestion maps only `${extensionPath}` to `${CLAUDE_PLUGIN_ROOT}` and preserves project-directory tokens as written.
- Factory MCP ingestion canonicalizes `${DROID_PLUGIN_ROOT}`, and Factory MCP compilation emits it, so Factory round trips and cross-harness conversions use each harness's own plugin-root token.

## Capabilities

### Modified Capabilities

- `harness-compilation`: project and plugin roots stay distinct, and plugin-root tokens are translated between harnesses.

## Impact

Gemini and Factory ingestion and Factory MCP compilation in `pkg/harness-compilation`. A cross-harness conversion of a Gemini project-directory reference now keeps the original token instead of pointing into the plugin. No fixture executes a resource.
