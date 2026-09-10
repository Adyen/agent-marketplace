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

## Native assertions and remaining limits

Skills are checked by native name and description, and by source path when the CLI exposes a location field. MCP checks require both names and reported local connections. No Gemini model turn is run.

The other resource kinds are not omitted merely because their commands start with `/`:

- **Commands:** `/commands list` exists in a session, but the headless startup authenticates before dispatching slash commands.
- **Subagents:** `/agents list` is likewise session-scoped, not a top-level credential-free inventory command.
- **Hooks:** `/hooks list` is session-scoped. The top-level `gemini hooks` CLI exposes migration, not a listing subcommand.
- **Rules:** the compiler explicitly reports the lack of a target rule primitive and suppresses the artifact.
- **Collections:** virtual Marketplace selections, validated through their expanded resources.

Generated layouts and compiler diagnostics still cover those cases. Sending a prompt that happens to look like a slash command is not an acceptable replacement for a verified no-model inspection path.

References: [command reference](https://geminicli.com/docs/reference/commands/), [headless startup authentication](https://github.com/google-gemini/gemini-cli/blob/v0.59.0/packages/cli/src/gemini.tsx), [terminal hooks subcommands](https://github.com/google-gemini/gemini-cli/blob/v0.59.0/packages/cli/src/commands/hooks.tsx).
