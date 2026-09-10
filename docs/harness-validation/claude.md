# Claude Code validation

[Shared validation contract](README.md)

## Installed resources

Skills, commands, and subagent definitions are installed below the scope's `.claude` directory. MCP servers are installed at `<repo>/.mcp.json` or `~/.claude.json#mcpServers`. Native installation is distinct from compiling a plugin bundle; validation does not register an alternate plugin or inject replacement definitions to make discovery succeed.

## Skills, commands, and subagents: CLI initialization

The driver starts the installed `claude` executable with:

```text
--print --input-format stream-json --output-format stream-json
--verbose --setting-sources user,project,local
```

It sends one control request on stdin:

```json
{"type":"control_request","request_id":"amcompat-initialize","request":{"subtype":"initialize"}}
```

This is the CLI protocol used by the Agent SDK. No SDK dependency, user prompt, custom `--agents` definition, or model turn is supplied. After reading the matching successful response, the driver closes stdin and requires a successful process exit.

- **Skills and commands:** inspect the response's `commands` inventory. Each fixture name must appear exactly once and retain its fixture description. Claude can add presentation text to command descriptions; the assertion still requires the complete expected description content.
- **Subagents:** inspect the `agents` inventory, requiring the exact fixture name and description.
- **Negative control:** run the same initialization before installation; the fixture entries must be absent from those inventories.

Initialization lists available resources, not resources invoked by a model. A command name appearing elsewhere in an error or startup message does not satisfy the inventory assertion.

Implementation: [probe selection](../../e2e/harness_compatibility_coverage_test.go), [control transport](../../e2e/harness_compatibility_protocol_test.go), and [inventory assertions](../../e2e/harness_compatibility_inventory_test.go).

## MCP servers

The driver runs `claude --version`, `claude mcp list`, and `claude mcp get <fixture-name>`. Before installation the fixture names must be absent. After installation both names must be present, and the list must report both local stdio fixtures connected. The `get` checks require server identity, not unexposed executable/environment values; generated configuration is checked separately.

For repository scope, the driver first verifies pending approval and no established fixture connections. It then trusts only the disposable workspace in its isolated user config and adds only the two fixture server names to untracked workspace settings. User-scope MCP entries do not use this project approval transition. Agent Marketplace itself never grants trust.

## Remaining limits

- **Hooks:** initialization does not return an inventory of installed hooks. SDK `hook_callback` messages are calls to client-provided callbacks, not proof that an on-disk hook was discovered. Hook output remains configuration-verified; the fixture asserts that inspection did not fire it.
- **Rules:** the compiler reports the absence of this target's rule primitive and suppresses the artifact.
- **Collections:** virtual Marketplace selections; validation covers their expanded members.

Do not substitute `claude agents --json`: that command lists background sessions, not custom subagent definitions.

Reference: [Agent SDK initialization and resource metadata](https://code.claude.com/docs/en/agent-sdk/typescript).
