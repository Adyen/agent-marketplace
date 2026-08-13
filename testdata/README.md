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

Add to `.agents/config.yaml`:

```yaml
# MCP servers only
registries:
  - url: ./testdata/example-mcp-registry
    mcp:
      include:
        - example-echo
        - example-search

# Skills only
registries:
  - url: ./testdata/example-skills-registry

# Combined (skills + MCP in one registry)
registries:
  - url: ./testdata/example-combined-registry

# Collections
registries:
  - url: ./testdata/local-registry
collections:
  - name: agent-marketplace-tools
```

Then:

```sh
agent-marketplace sync
agent-marketplace list --kind mcp
agent-marketplace status
```
