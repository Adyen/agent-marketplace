# Devin CLI validation

## Native installation contract

Devin uses:

- Project root: `<repo>/.devin`
- User root: `~/.config/devin`
- Skills and converted commands: `skills/<name>/SKILL.md`
- Subagents: `agents/<name>.md`
- Project hooks: `hooks.v1.json`
- User hooks: the `hooks` member of `config.json`
- MCP: `mcp_config.json`

Native hook groups contain an event-level matcher group with a nested `hooks` array. Legacy flat hook input remains ingestible. Sync upgrades only entries supported by durable ownership evidence and preserves handwritten groups/settings.

## Native probe

The CI image installs a pinned Devin bundle and disables auto-update. The probe runs:

```text
devin --version
devin skills list
devin skills show <name>
devin mcp list
devin mcp get <name>
```

Skills and converted commands are natively inspected by name, description, and source path. MCP entries are inspected for names and configured fields; transport execution is not claimed by the offline probe.

## Configuration-only coverage

Hooks and custom subagents are validated through generated files and compiler/ingester contracts. Devin has no suitable credential-free read-only hook execution surface, so the test does not claim that hooks fired. Known session-only or authenticated gaps remain listed by the existing Devin probe documentation.

## Safety requirements

The runtime probe must use an isolated HOME, XDG roots, repository, and state directory. It must never authenticate, invoke a model, mutate a real Devin configuration, or execute an external MCP service. Malformed aggregate files must fail closed and must not damage unrelated aggregates.
