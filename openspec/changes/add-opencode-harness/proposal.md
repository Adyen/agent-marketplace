## Why

OpenCode users cannot currently select it as a harness target or import its source layout through Agent Marketplace. Adding full harness support must preserve the existing security boundaries and prove native hook behavior before release, so translation cannot silently weaken execution restrictions or ownership guarantees.

## What Changes

- Add opt-in `opencode` selection across configuration, initialization, TUI, status, doctor, backup, and bidirectional conversion while keeping existing defaults unchanged.
- Compile and ingest native skills, commands, subagents, MCP servers, and a documented supported hook subset in repository and machine scopes, including supported custom roots.
- Require first-release native execution evidence for hooks, including activation, blocking, failures, cancellation, and consumer-path coverage. Unsupported individual forms fail closed; postponing the entire hook resource kind is not an acceptable release.
- Reconcile OpenCode JSON/JSONC MCP entries through durable ownership state, private backups, journaled publication, and safe repair while preserving handwritten configuration.
- Preserve effective permissions, literal/reference meaning, strict parsing, descriptor-based filesystem safety, bounded reads, exact acquisition authority, and incomplete-input preservation through every entry point.
- Diagnose rules and arbitrary executable plugins as unsupported without executing source code or changing shared instructions, trust, authentication state, or consumer-wide permission policy.

## Capabilities

### New Capabilities

- `harness-opencode`: Selection, source-layout detection, native resource translation, faithful permissions, diagnostics, inspection, and compatibility for OpenCode full harness support.
- `opencode-hooks`: Safe declarative hook translation and ingestion, additive contributions, native execution semantics, and mandatory first-release proof.
- `opencode-native-configuration`: Native root/configuration resolution, ownership-reconciled MCP installation, read-only inspection, backup, and repair in OpenCode scopes.

### Modified Capabilities

None. Existing materialization and owned-entry reconciliation requirements remain applicable; the new capabilities specify OpenCode behavior without relaxing those contracts.

## Impact

The target registry, harness-compilation compilers/ingesters, installation adapters, native configuration patching, generic materialization consumers, CLI/TUI selection, Marketplace assessment/inspection, backups, diagnostics, documentation, and compatibility gates are affected. Production workflows retain the Go-only installation model and do not require or execute an OpenCode, Bun, Node, or npm binary. Dedicated hermetic native probes require a verified pinned OpenCode executable and reviewed synthetic fixtures.

No acquisition-lock format, existing source-revision format, default target list, wire format, or existing harness behavior is intentionally changed. The initial investigation uses OpenCode v1.18.34; implementation must establish its supported version and prove the contract before advertising support.
