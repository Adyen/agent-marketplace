# Claude Code validation

[Shared validation contract](README.md)

## Installed resources

Skills, commands, subagent definitions, and hooks are installed below the
scope's `.claude` directory. Hooks merge into `settings.json` under `hooks`, in
the native event, matcher-group, and handler-array shape. MCP servers are
installed at `<repo>/.mcp.json` or `~/.claude.json#mcpServers`. Native
installation is distinct from compiling a plugin bundle. Validation does not
register an alternate plugin or inject replacement definitions to make
discovery succeed.

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

This is the CLI protocol that the Agent SDK uses. The driver supplies no SDK
dependency, user prompt, custom `--agents` definition, or model turn. After it
reads the matching successful response, it closes stdin and requires a
successful process exit.

- Skills and commands: the driver inspects the response's `commands`
  inventory. Each fixture name must appear exactly once with its fixture
  description. Claude can add presentation text to command descriptions, but
  the assertion still requires the complete expected description.
- Subagents: the driver inspects the `agents` inventory and requires the
  exact fixture name and description.
- Negative control: the same initialization runs before installation, and the
  fixture entries must be absent from those inventories.

Initialization lists the resources that are available, not resources a model
invoked. A command name that shows up elsewhere, in an error or startup
message, does not satisfy the inventory assertion.

Implementation: [probe selection](../../e2e/harness_compatibility_coverage_test.go),
[control transport](../../e2e/harness_compatibility_protocol_test.go), and
[inventory assertions](../../e2e/harness_compatibility_inventory_test.go).

## MCP servers

The driver runs `claude --version`, `claude mcp list`, and
`claude mcp get <fixture-name>`. Before installation the fixture names must be
absent. After installation both names must be present, and the list must
report both local stdio fixtures as connected. The `get` checks require the
server identity only, because Claude does not expose the executable and
environment values there; the generated configuration is checked separately.

For repository scope, the driver first verifies that approval is pending and
no fixture connection is established. It then trusts only the disposable
workspace in its isolated user config and adds only the two fixture server
names to untracked workspace settings. User-scope MCP entries skip this
project approval step. Agent Marketplace itself never grants trust.

## Hooks

Repository hooks are in `<repo>/.claude/settings.json#hooks`; user hooks are in
`~/.claude/settings.json#hooks`. Support files install below the scope's
`.claude/hook-support/<source>/<hook>/` directory. The fixture checks the
settings content, preservation of unrelated settings and handwritten hooks, and
support-file bytes. It does not run Claude through a tool call that would fire
a hook.

A legacy `.claude/hooks/hooks.json` may remain as a conversion artifact or a
source layout. During sync migration, only entries already marked
`_managed_by: agent-marketplace` move from that file; handwritten entries stay.

## Remaining limits

- Hooks: initialization does not return an inventory of installed hooks. SDK
  `hook_callback` messages call client-provided callbacks and do not prove
  that an on-disk hook was discovered. Hook output stays
  configuration-verified, and the fixture asserts that inspection did not fire
  it.
- Rules: the compiler reports that this target has no rule primitive and
  suppresses the artifact.
- Collections: virtual Marketplace selections; validation covers their
  expanded members.

Do not substitute `claude agents --json`. That command lists background
sessions, not custom subagent definitions.

Reference: [Agent SDK initialization and resource metadata](https://code.claude.com/docs/en/agent-sdk/typescript).
