# Registry layout reference

## Layout by kind

| Kind | Directory | Expected files |
|---|---|---|
| skill | `skills/<name>/` | `SKILL.md` with top-level `name`, `description`, and optional `license`; `metadata` owns `tags` and `requires`. |
| command | source-harness-specific, e.g. `commands/<name>.md` for Claude or `skills/<name>/` with `x-agent-marketplace-kind: command` for Devin/Codex | Native command file or skill-shaped command for the source harness. |
| subagent | source-harness-specific; see the table below | Native subagent file for the source harness. |
| hook | source-harness-specific; see the table below | Native hook file or aggregate for the source harness. |
| mcp | source-harness-specific; see the table below | Native MCP file or aggregate for the source harness. |
| collection | `collections/` | `collections/<name>/COLLECTION.yaml`. |

There are no generic `subagents/` or `mcp/` discovery roots. Ingestion looks for each source harness's native layout, so a subagent named `review` ships as `agents/review.md` in a Claude registry, not `subagents/review.md`, and MCP servers ship as a root-level `.mcp.json` in a Claude registry, not `mcp/mcp.json`. Paths placed in unrecognized directories are not discovered.

### Source paths by harness

| Harness | Subagent | Hook | MCP server |
|---|---|---|---|
| Claude Code | `agents/<name>.md` | `hooks/hooks.json` | `.mcp.json` at the bundle root |
| Factory Droid | `droids/<name>.md` | `hooks/hooks.json` | `mcp.json` at the bundle root |
| Cursor | `agents/<name>.md` | `hooks/hooks.json` | `mcp.json` at the bundle root |
| Windsurf | not discovered | `hooks/<name>.json` or `hooks.json` | `mcp_config.json` |
| Gemini CLI | `.gemini/agents/<name>.md` | `.gemini/settings.json` under `hooks` | `gemini-extension.json`, `.gemini/mcp.json`, or `.gemini/settings.json` under `mcpServers` |
| Devin CLI | `agents/<name>.md` | `hooks.v1.json` (project layout) or `config.json` under `hooks` | `mcp_config.json` |
| Codex | `.codex/agents/<name>.toml` | `.codex/hooks.json` | `.codex/config.toml` under `mcp_servers` |

Workspace layouts under each harness root are also recognized: `.claude/`, `.cursor/`, `.factory/`, `.windsurf/`, and `.devin/` prefix the same relative paths (for example `.claude/agents/<name>.md`). Codex sources always use the `.agents/` and `.codex/` partitions shown above.

The CLI attempts to autodetect the source layout for subagent, hook, and MCP files. Set `source_harness` when the registry is ambiguous.

## Source harness

Set `source_harness` on the registry entry when the layout is ambiguous:

```yaml
registries:
  - url: https://github.com/example-org/agent-resources.git
    ref: v1.4.0
    source_harness: claude
```

## Ingestion roots

A registry entry can limit discovery to one or more subdirectories with `path`. For example, `path: resources` discovers only `resources/skills/...`, `resources/commands/...`, and so on.

When operating a hosted Marketplace, `roots` declare ingestion paths relative to the registry origin.

## Conversion direction

Every supported harness can be a conversion source and a conversion destination. See the [how to convert a harness layout](../how-to/convert-a-harness-layout.md) guide.

## Related documentation

- [How to add a resource to a registry](../how-to/author/add-a-resource-to-a-registry.md)
- [Resource metadata reference](./resource-metadata.md)
- [Collections reference](./collections.md)
