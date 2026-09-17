# Resource discovery and compilation

## Discovery

For each configured registry, the CLI:

1. Resolves the Git ref to the exact commit in the lock.
2. Opens the snapshot and walks the ingestion roots.
3. Finds skills, commands, subagents, hooks, and MCP servers by their native layout signatures.
4. Reads collection definitions and expands them into synthetic registry entries.

## Ingestion

Ingestion converts a native source file into a canonical intermediate representation. The IR is independent of any harness. Each IR resource represents one individual resource kind: a skill, a command, a subagent, a hook, or an MCP server. No single IR resource carries fields for every kind.

## Selection

After discovery, the CLI applies:

- target harness intersection;
- per-kind selectors;
- tags;
- collections;
- exclusions and precedence rules.

The result is a set of resources to compile for each target.

## Compilation

For each selected resource and each target harness, the compiler:

1. Maps the IR fields to the target's native schema.
2. Emits any diagnostics for unsupported fields.
3. Produces one or more artifacts.

Artifacts are byte-stable for deterministic materialization.

## Materialization

The CLI writes artifacts to the repository or machine. It then removes stale generated output and reconciles shared configuration files.

## Honest diagnostics

When a target cannot represent a value, the compiler emits a stable `code` and may skip the affected entry. The rest of the run continues.

## Related documentation

- [Native harness materialization](./native-harness-materialization.md)
- [Registry layout reference](../reference/registry-layout.md)
- [Resource metadata reference](../reference/resource-metadata.md)
