# Diagnostics reference

This page describes the structured diagnostics `agent-marketplace` emits during sync, materialization, and conversion.

## Diagnostic structure

Every diagnostic has:

- A stable dot-separated `code`.
- A `severity`: `error`, `warning`, or `info`.
- A `summary` describing the condition.
- A `hint` suggesting the next action.

## Severity semantics

- `error`: the affected resource or file cannot be emitted or the run cannot continue in that scope.
- `warning`: the run continues, but some data was dropped or adjusted.
- `info`: a note about a normal transformation.

## Common prefixes

| Prefix | Topic |
|---|---|
| `ingest.*` | Converting a native harness file into canonical IR. |
| `compile.*` | Converting canonical IR into a target harness. |
| `skill.*`, `subagent.*`, `hook.*`, `mcp.*` | Resource-specific translation issues. |
| `ownership.*` | Devin or Codex aggregate ownership. |
| `materialize.*` | Writing generated files. |
| `marketplace.*` | Hosted Marketplace policy or ingestion. |
| `doctor.*` | Diagnostics from `agent-marketplace doctor`. |
| `rule.*` | Coding convention rules unsupported for a target. |
| `sync.*` | Sync lifecycle. |
| `lock.*` | Lockfile state. |
| `dependency.*` | Pending transaction recovery. |

## Viewing diagnostics

- `agent-marketplace sync -v` prints affected files and malformed-resource details.
- `agent-marketplace status` prints diagnostics in the report.
- `agent-marketplace doctor` prints ownership and compatibility diagnostics.

## Complete catalog

The full catalog of `harness-compilation` diagnostic codes is maintained in the source tree at `pkg/harness-compilation/diag/codes.md`.

## Related documentation

- [How to troubleshoot sync drift](../how-to/troubleshoot-sync-drift.md)
- [How to repair owned shared configuration](../how-to/repair-owned-shared-configuration.md)
