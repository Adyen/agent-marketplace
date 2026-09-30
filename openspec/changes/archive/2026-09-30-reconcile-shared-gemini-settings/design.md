## Context

Native Gemini MCP and hook routes share settings.json. Per-output-key reconciliation currently treats the other scope's owned entries as stale when HOME is the repository root.

## Goals / Non-Goals

- Reconcile shared native routes once without combining unrelated skill/file plans.
- Preserve ownership proof, handwritten content, modes, no-ops and failure fences.
- Do not change other harness layouts, filesystem authorization or configuration formats.

## Decisions

Preflight shared Gemini roots before any writes. Transfer only the registered native aggregate fragments to one deterministic scope and suppress the other native pass. Validate keyed MCP values before merging, and combine incomplete-compilation fences conservatively. Scope-restricted operations refuse because durable entry proof does not independently partition the shared file by configuration scope.

## Risks / Trade-offs

Do not use a last-wins fragment merge as a cross-scope authority decision. Same-key equal values are compatible; differing values fail before publication. Valid independent destinations remain unchanged. This does not provide a general filesystem transaction or close the separate descriptor-containment work.
