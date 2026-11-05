# Test Registries

Local (non-git) registries for testing agent-marketplace features without a remote.

## Layout

```text
testdata/
├── local-registry/                  ← collections test registry
│   └── collections/
│       └── agent-marketplace-tools/
│           └── COLLECTION.yaml
├── example-mcp-registry/            ← MCP servers only (Factory format)
│   └── mcp.json
├── example-skills-registry/         ← skills only (Claude format)
│   └── skills/greeting/SKILL.md
└── example-combined-registry/       ← skills + MCP servers together
    ├── skills/code-explainer/SKILL.md
    └── mcp.json
```

## MCP Servers

The MCP registries use the standardized `mcpServers` JSON format that all
harnesses understand (Factory, Claude, Cursor, Windsurf, Gemini):

```json
{
  "mcpServers": {
    "server-name": {
      "command": "/usr/local/bin/my-mcp-server",
      "args": ["--port", "8080"],
      "env": { "KEY": "value" }
    }
  }
}
```

### MCP Config Merge

Managed MCP entries (synced via agent-marketplace) are tagged with
`"_managed_by": "agent-marketplace"`. On sync, the `.factory/mcp.json` is
**merged**, not overwritten:

- User-owned entries (no `_managed_by`) are preserved
- Stale managed entries (tagged but no longer in config) are removed
- Current managed entries are added/updated
- The file is always a regular file, never a symlink

## Using the test registries

Declare them as `directory` sources in `.agents/config.yaml` (relative paths resolve from the repository root) and select resources per source:

```yaml
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  mcp-examples:
    directory: ./testdata/example-mcp-registry       # MCP servers only
  skill-examples:
    directory: ./testdata/example-skills-registry    # skills only
  combined-examples:
    directory: ./testdata/example-combined-registry  # skills + MCP servers
  local-tools:
    directory: ./testdata/local-registry             # collections
resources:
  mcp-examples: [mcp/example-echo, mcp/example-search]
  skill-examples: {all: true}
  combined-examples: {all: true}
  local-tools: [collection/agent-marketplace-tools]
```

Then:

```sh
agent-marketplace sync
agent-marketplace list --kind mcp
agent-marketplace status
```
