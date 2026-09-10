# Codex validation

## Native installation contract

Codex is a partitioned target:

- Skills and converted commands: `<repo>/.agents/skills/` or `~/.agents/skills/`
- Subagents: `<repo>/.codex/agents/` or `$CODEX_HOME/agents/`
- Hooks: `.codex/hooks.json`
- MCP: `.codex/config.toml`

`CODEX_HOME` relocates only the machine `.codex` partition. It never relocates skills. Repository partitions must remain inside the repository; machine destinations require absolute roots.

## Native probe

The CI image installs a pinned `codex` CLI. The probe runs:

```text
codex --version
codex mcp list --json
codex mcp get <name> --json
```

Before materialization, both fixture names must be absent. After `sync`, both names must appear in the list, and each `get` result must expose the name, fixture executable, label argument, and fixture environment setting.

Codex validation currently covers native MCP configuration recognition only. The report marks connection status `not-probed`; no model session or MCP tool call is executed.

## Trust boundary

Codex ignores project `.codex` configuration until the project is trusted. For repository scope, CI first verifies that the untrusted project does not expose the fixture MCP entries. It then writes a trust entry for only the disposable workspace in the isolated user config and repeats the native probe.

User-scope MCP validation does not require repository trust because the configuration is machine-scoped. The application never changes Codex trust; fixture trust exists only inside the CI sandbox.

## Configuration-only coverage

Skills, commands, subagents, hooks, collections, and rules are validated through Agent Marketplace paths, aggregate files, materialization modes, and compiler diagnostics. Their execution is not claimed by `codex mcp list`.

## Failure interpretation

- Missing MCP names before installation fail the negative-control contract.
- Missing names after installation indicate a destination, scope, trust, or native parser problem.
- A successful CLI exit without the expected names is still a failure.
- Trust state must preserve unrelated user configuration and must not escape the disposable fixture root.
