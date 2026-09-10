# Gemini CLI validation

## Native installation contract

Gemini MCP servers and hooks share the native settings file:

- Repository scope: `<repo>/.gemini/settings.json`
- User scope: `~/.gemini/settings.json`

MCP entries belong under `mcpServers`; hook entries use Gemini's matcher-group shape, with an outer event group containing a `hooks` array. Gemini MCP server definitions must not contain Agent Marketplace's `_managed_by` marker because Gemini validates server properties strictly. Ownership is kept in the durable Agent Marketplace journal instead.

The older plugin-style `gemini-extension.json` output remains ingestible/compatible, but native validation uses `settings.json` and does not silently register an incomplete extension document.

## Native probe

The CI image contains the pinned `gemini` CLI. The probe runs:

```text
gemini --version
gemini skills list
gemini mcp list
```

Before synchronization, fixture names must be absent. After synchronization:

- user-scope skills must be listed
- repository-scope skills must be listed after fixture-only trust
- both MCP names must be listed
- both local stdio MCP servers must report connected after trust setup

The generated settings file must also preserve unrelated settings and hooks.

## Trust boundary

Gemini's workspace trust affects more than repository resources: in an untrusted workspace it can suppress even user-level MCP connections. CI therefore records the untrusted state first, then writes a single `TRUST_FOLDER` rule for the temporary workspace under the isolated HOME. The application never changes trust. A final `sync --check` ensures the trust operation did not create Agent Marketplace drift.

## Configuration-only coverage

Commands, subagents, hooks, collections, and rules are checked through generated layouts and compiler diagnostics. No Gemini model turn is run. The native MCP check is configuration plus local-fixture connection evidence, not proof that a model invoked a tool.
