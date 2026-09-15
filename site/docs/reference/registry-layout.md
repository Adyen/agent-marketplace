# Registry layout reference

This page describes the directory structure a Git registry can use so `agent-marketplace` discovers its resources.

## Layout by kind

| Kind | Directory | Expected files |
|---|---|---|
| skill | `skills/<name>/` | `SKILL.md` with top-level `name`, `description`, and optional `license`; `metadata` owns `tags` and `requires`. |
| command | source-harness-specific, e.g. `commands/<name>.md` for Claude or `skills/<name>/` with `x-agent-marketplace-kind: command` for Devin/Codex | Native command file or skill-shaped command for the source harness. |
| subagent | `subagents/` | Source-harness-specific native files; see the [harnesses and output paths reference](./harnesses-and-output-paths.md) for exact paths. |
| hook | `hooks/` | Source-harness-specific native hook files. |
| mcp | `mcp/` | Source-harness-specific native MCP files. |
| collection | `collections/` | `collections/<name>/COLLECTION.yaml`. |

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
