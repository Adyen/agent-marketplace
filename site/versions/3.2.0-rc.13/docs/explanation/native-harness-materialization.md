# Native harness materialization

`agent-marketplace` does not impose a single output format. Instead, it compiles the same canonical resource into the native files each harness expects.

## Why native layouts

Coding agents read their own formats: Claude Code expects `.claude/skills/<name>/SKILL.md`, Factory Droid expects `.factory/skills/<name>/SKILL.md`, Devin CLI expects `.devin/skills/<name>/SKILL.md` with a `SKILL.md` file and a `x-agent-marketplace-kind: command` marker for commands. Native output means the generated files work immediately, without an intermediate adapter.

## The compilation pipeline

1. **Ingest**: read the source resource in its native harness layout and translate it into a canonical intermediate representation.
2. **Canonical IR**: each intermediate value represents one resource (a skill, command, subagent, hook, or MCP server) and is independent of any harness.
3. **Compile**: translate the canonical IR into the target harness artifacts.
4. **Materialize**: write the artifacts to disk atomically and prune stale output.

## Honest about loss

When a target cannot represent a field or value, the compiler emits a stable diagnostic and may skip that part of the resource. It never silently invents data.

## Link versus file

In the default `symlink` mode, the CLI creates links to the compiled cache for skills and converted command directories. In `file` mode, the CLI writes regular files and trees. Shared configuration files such as `.codex/hooks.json` and `.devin/mcp_config.json` are always ownership-reconciled, not linked.

## Ownership and shared aggregates

Devin and Codex store durable ownership in XDG state so the CLI can distinguish generated entries from hand-authored ones. This lets `sync` preserve user edits while still updating managed entries.

## Related documentation

- [Harnesses and output paths reference](../reference/harnesses-and-output-paths.md)
- [How to convert a harness layout](../how-to/convert-a-harness-layout.md)
- [Diagnostics reference](../reference/diagnostics.md)
