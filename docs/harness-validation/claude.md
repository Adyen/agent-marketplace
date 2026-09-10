# Claude Code validation

## Native installation contract

Agent Marketplace installs MCP servers into Claude's native locations:

- Repository scope: `<repo>/.mcp.json`
- User scope: `~/.claude.json`, under `mcpServers`

The plugin-shaped `.claude/.mcp.json` output remains supported for compilation and ingestion compatibility, but native validation does not treat it as a registered workspace MCP configuration. Legacy plugin-style files are not implicitly moved or deleted.

## Native probe

The CI image contains the pinned `claude` CLI. The probe runs:

```text
claude --version
claude mcp list
claude mcp get <fixture-name>
```

The negative control requires both fixture names to be absent before synchronization. After synchronization, both names must appear. The list must report both local stdio fixture servers as connected after the isolated approval setup. Each `get` result must expose the server identity; fields that the CLI does not display are not claimed as verified.

## Trust and approval

The test first observes the untrusted project state. It then writes trust and approval state only inside the disposable fixture:

- `~/.claude.json` receives a trust record for the temporary workspace.
- The untracked workspace settings receive an explicit allow-list containing only the two fixture server names.

The helper preserves unrelated JSON settings and refuses malformed or escaping paths. Agent Marketplace itself never grants Claude workspace trust or MCP approval.

## Configuration-only coverage

Skills, commands, subagents, and hooks are validated through generated files, paths, support files, and compiler diagnostics. The native CLI job does not claim that those resources executed in a model session. Rules are checked through the compiler/conversion path.

## Failure interpretation

- Missing MCP names indicate a native destination or approval problem, not a successful empty list.
- `Pending approval` before the fixture-only approval step is expected evidence of the trust boundary.
- A failed local fixture connection is distinct from a missing configuration entry.
- Existing handwritten `.mcp.json` or `~/.claude.json` entries must survive sync, and a name collision must fail closed.
