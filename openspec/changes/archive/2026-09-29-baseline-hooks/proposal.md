## Why

Hooks are the riskiest materialized resource kind: each one is a command line
that a harness target executes with the user's credentials. How hooks are
ingested, translated, placed, and reconciled is implemented and covered by
tests, but no spec describes it. Before changing hook behavior (see the
`native-hook-integration` change), this baseline records current behavior,
quirks included, as normative requirements bound to existing tests.

## What Changes

- Adds the `hook-resources` capability describing, as currently implemented:
  - ingestion of hooks from every source layout, including flattening,
    registry-local naming, duplicate-key rejection, and native event
    reversal;
  - the output file and scope for each harness target, including the
    plugin-layout `hooks/hooks.json` written into Claude Code, Cursor, and
    Factory Droid roots;
  - the fixed event translation table and the per-target severity of an
    unmappable event;
  - pass-through of tool matchers and timeout values without translation;
  - handler-type support per harness target;
  - plugin-root token handling per harness target;
  - support-file placement and conflict refusal;
  - shared-file ownership: preserving handwritten entries, pruning owned
    entries, and repeat-sync stability;
  - the Codex-only trust-review diagnostics;
  - additive hook selection across independent registries;
  - per-resource suppression, and value-free Devin hook diagnostics;
  - the Marketplace hook capability finding.
- No behavior changes. Every scenario describes current code and is proven by
  an annotated existing test. Quirks that the follow-up change fixes are
  specified as they behave today.

## Capabilities

### New Capabilities

- `hook-resources`: user-observable behavior of hook resources from source
  ingestion through selection, compilation, materialization into harness
  targets, and shared-file ownership.

### Modified Capabilities

None.

## Impact

- OpenSpec artifacts only, plus `// spec:` annotations on existing tests in
  `pkg/harness-compilation`, `internal/materialize`, `internal/resolver`,
  `internal/sourcerevision`, `pkg/marketplace/analyze`, and
  `cmd/agent-marketplace`.
- Absorbs the `docs/plans/2026-09-16-additive-hooks.md` row (4.17) and the
  `AGENTS.md` "Additive hook selection" appendix row (6.8) of
  `harvest-legacy-docs`, and deletes both.
- The `hook.*`, `ingest.*.hook-*`, and `materialize.*hook*` rows of
  `pkg/harness-compilation/diag/codes.md` and the hook rows of
  `site/docs/reference/harnesses-and-output-paths.md` remain descriptive
  references. The spec is normative where they disagree.
