# Gemini CLI validation

[Shared validation contract](README.md)

## Native installation contract

Gemini MCP servers and hooks share the native settings file:

- Repository scope: `<repo>/.gemini/settings.json`
- User scope: `~/.gemini/settings.json`

MCP entries go under `mcpServers`. Hook entries use Gemini's matcher-group
shape: an outer event group that contains a `hooks` array. New hook entries do
not carry `_managed_by`; Agent Marketplace records hook ownership in durable
state. Gemini validates server properties strictly, so Gemini MCP server
definitions must not contain Agent Marketplace's `_managed_by` marker. Agent
Marketplace records their ownership in its durable journal instead.

The older plugin-style `gemini-extension.json` output stays ingestible and
compatible. Native validation uses `settings.json` and does not silently
register an incomplete extension document.

Support files install under `.gemini/hook-support/<source>/<hook>/` in the
scope root. Gemini hook timeouts are milliseconds, and cross-harness
conversions scale seconds to milliseconds. A narrow compatibility migration
moves entries that a v3.1.1 Agent Marketplace installation marked in the older
flat settings shape into the grouped shape; it does not adopt unmarked hooks.

## Native probe

The CI image contains the pinned `gemini` CLI. The probe runs:

```text
gemini --version
gemini skills list
gemini mcp list
```

Before synchronization, the fixture names must be absent. After
synchronization:

- user-scope skills must be listed
- repository-scope skills must be listed after fixture-only trust
- both MCP names must be listed
- both local stdio MCP servers must report connected after trust setup

The generated settings file must also preserve unrelated settings and hooks.

## Trust boundary

Gemini's workspace trust reaches beyond repository resources: in an untrusted
workspace it can suppress even user-level MCP connections. CI therefore
records the untrusted state first, then writes a single `TRUST_FOLDER` rule
for the temporary workspace under the isolated HOME. The application never
changes trust. A final `sync --check` confirms that the trust change did not
create Agent Marketplace drift.

## Native assertions and remaining limits

The probe checks skills by native name and description, and also by source
path when the CLI prints a location field. MCP checks require both names and
reported local connections. No Gemini model turn runs.

A slash-command form alone is not the reason the other resource kinds lack a
native probe:

- Commands: `/commands list` exists in a session, but headless startup
  authenticates before it dispatches slash commands.
- Subagents: `/agents list` is also session-scoped, not a top-level
  credential-free inventory command.
- Hooks: `/hooks list` is session-scoped, and the top-level `gemini hooks` CLI
  offers migration, not a listing subcommand.
- Rules: the compiler reports that the target has no rule primitive and
  suppresses the artifact.
- Collections: virtual Marketplace selections, validated through their
  expanded resources.

Generated layouts and compiler diagnostics still cover these cases. Sending a
prompt that happens to look like a slash command is not an acceptable
substitute for a verified inspection path that runs no model.

References: [command reference](https://geminicli.com/docs/reference/commands/), [headless startup authentication](https://github.com/google-gemini/gemini-cli/blob/v0.59.0/packages/cli/src/gemini.tsx), [terminal hooks subcommands](https://github.com/google-gemini/gemini-cli/blob/v0.59.0/packages/cli/src/commands/hooks.tsx).
