## Why

Resources can declare `metadata.requires`, but unmet prerequisites are currently advisory and never participate in selection. An opt-in, default-off resolver path can expand those prerequisites without persisting implied selections, while keeping release-N behavior compatible and safely tolerating existing materialized preview outputs.

## What Changes

- Add the v4 `requires.expand` setting with layered precedence and a default of `false`; legacy configuration receives the same default and migration does not seed the setting.
- When enabled, resolve the transitive closure of configured resource prerequisites, preserving source-hint, scope, harness-target, exclusion, required-selection, and direct-selection precedence. Track prerequisite provenance separately from collection provenance.
- When disabled, compute implied resources as a read-only preview. Keep selection and strict-sync behavior unchanged, report typed unmet reasons, and preserve only matching existing preview outputs and ownership without creating, rewriting, adopting, or broadly suppressing stale-output pruning.
- When expansion is enabled, allow removal of an optional resource that remains required by writing an explicit exclude and warning dependents, without cascading removal or adding a new flag.

## Capabilities

### New Capabilities

- `resource-removal`: Define removal of an optional resource that remains required, including the enabled-mode exclude and dependent warning behavior.

### Modified Capabilities

- `resource-prerequisites`: Add opt-in transitive expansion, configured-source lookup, scope and exclusion precedence, separate prerequisite provenance, advisory preview behavior, and typed unmet reasons while preserving existing warning and strict-sync semantics.
- `configuration-settings`: Add the v4 `requires.expand` setting, its layered merge/default behavior, legacy default, and migration constraints.
- `materialization`: Preserve only exact existing managed outputs corresponding to admitted-scope advisory previews, without creating or rewriting preview outputs, adopting unmanaged entries, or suppressing unrelated pruning.

## Impact

Resolver and configuration settings; sync/check and removal pipelines; scoped file, link, manifest, and aggregate reconciliation; OpenSpec scenarios; focused resolver/config/materialization/pipeline/command tests, property tests, and local-Git E2E coverage; and relevant reference, how-to, and glossary documentation. No Marketplace dependency lookup or requirement-edge writes, default flip, slice-2 CLI/TUI surfaces, or hook/MCP requirement authorship are included.
