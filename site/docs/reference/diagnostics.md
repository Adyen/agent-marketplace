# Diagnostics reference

A [diagnostic](../glossary.md#diagnostic) is a structured message that `agent-marketplace` reports when something about a resource, a file, or a run needs your attention. This page explains the parts of a diagnostic, what each severity means, and how to find the code you are looking at. Use it when a command prints a code such as `lock.stale` or `rule.devin.unsupported` and you want to know which area it comes from.

## Diagnostic structure

Diagnostics produced while converting and writing resources have these fields:

- `code`: a stable, dot-separated identifier, such as `subagent.windsurf.no-primitive`. Scripts and tests should match on the code, never on the message text.
- `severity`: `error`, `warning`, or `info`.
- `target`: the harness the diagnostic applies to, or empty when it applies to every harness.
- `resource`: the name of the resource that produced it.
- `message`: a readable description of the condition.
- `hint`: an optional suggestion for what to do next.
- `source`: the original file, relative to the registry or bundle root, and the line number when known.

Configuration, lock, and dependency errors also start their message with a stable code, for example `lock.stale: repo lock "<repo>/.agents/lock.yaml": lock does not match configuration`.

## Severity semantics

- `error`: the source is malformed or unsafe. The affected resource or file is not written, or the run cannot continue in that scope.
- `warning`: the run continues, but the resource was translated with some loss or was dropped.
- `info`: the translation succeeded with a change worth noting.

## Common prefixes

The first part of a code names the area that reported it.

| Prefix | Topic |
|---|---|
| `ingest.*` | Reading a native harness file into the internal resource model. |
| `compile.*` | Writing the internal resource model out for a target harness. |
| `skill.*`, `subagent.*`, `hook.*`, `mcp.*` | Translation issues for one resource kind. |
| `rule.*` | Harness rule files that a target does not support, such as `rule.devin.unsupported`. |
| `bundle.*` | Conflicts between resources in one bundle, such as two resources writing the same path. |
| `json.*` | Malformed or duplicate-key JSON in a harness file. |
| `materialize.*` | Writing generated files into harness folders. |
| `ownership.*` | Ownership records and locking for Devin and Codex shared configuration files. |
| `doctor.*` | Findings from `agent-marketplace doctor` and `doctor --repair`. |
| `config.*` | Loading and editing configuration files, such as `config.legacy-deprecated`. |
| `v4.*` | Validation of v4 configuration files, such as `v4.writer-opt-in`. |
| `lock.*` | Lockfile state, such as `lock.stale` or `lock.snapshot-unavailable`. |
| `dependency.*` | Recovery of an interrupted config and lock update, such as `dependency.recovery-required`. |
| `git.*` | Git acquisition and authentication, such as `git.auth-config-untrusted`. |
| `catalog.*`, `resolver.*` | Discovering resources in registries and choosing between them, such as `resolver.name-collision`. |
| `marketplace.*` | Hosted Marketplace security findings, policy, and ingestion. |

## Viewing diagnostics

- `agent-marketplace sync` prints errors and warnings. Add `-v` to also see affected files and malformed-resource details.
- `agent-marketplace status` prints warnings and errors in its `WARNINGS` and `ERRORS` sections.
- `agent-marketplace doctor` prints ownership and compatibility diagnostics.
- In the TUI, press `e` to open the diagnostics view.

## Complete catalog

The full list of harness-compilation diagnostic codes, with their severities and examples, is in the source tree at [`pkg/harness-compilation/diag/codes.md`](https://github.com/Adyen/agent-marketplace/blob/main/pkg/harness-compilation/diag/codes.md).

## Related documentation

- [How to troubleshoot sync drift](../how-to/troubleshoot-sync-drift.md)
- [How to repair owned shared configuration](../how-to/repair-owned-shared-configuration.md)
