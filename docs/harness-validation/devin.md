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

- **Hooks:** Devin documents `/hooks` as an in-session inventory. There is no documented standalone hook-listing subcommand for the credential-free driver. SDK-provided hooks or merely parsing user settings would not prove discovery of the project hook file.
- **Subagents:** custom profiles are used by `run_subagent`; the inspected CLI reference does not expose a standalone profile-definition inventory. Starting an agent is not a metadata-only check.
- **Rules:** `devin rules list/show` exists, but this compiler intentionally does not emit Devin rules. The suite tests the explicit omission diagnostic rather than pretending that a negative-only rule check proves support.
- **Collections:** virtual Marketplace selections, not native resources.

The ACP interface was also considered: it documents credential acquisition and a session command palette, not a credential-free hook/profile inventory. Hooks and subagents therefore remain covered by generated-file and compiler/ingester contracts; they are not certified as natively invoked.

References: [Devin command reference](https://docs.devin.ai/cli/reference/commands), [hook verification](https://docs.devin.ai/cli/extensibility/hooks/overview).

## Safety requirements

The runtime probe must use an isolated HOME, XDG roots, repository, and state directory. It must never authenticate, invoke a model, mutate a real Devin configuration, or execute an external MCP service. Malformed aggregate files must fail closed and must not damage unrelated aggregates.
