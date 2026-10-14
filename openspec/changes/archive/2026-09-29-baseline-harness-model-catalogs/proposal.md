## Why

Model and reasoning conversion for skills and subagents is already implemented
and user-observable — compiled artifacts gain or lose native model/effort
fields, diagnostics report the losses, ingestion retains native spellings with
source provenance, and source-revision digests encode that provenance — but no
spec describes any of it. This baseline change records the current behavior as
normative requirements bound to the tests that already prove it.

## What Changes

- Adds the `harness-model-catalogs` capability describing, as implemented:
  - joint source-aware model/reasoning resolution driven by shipped
    per-harness-target catalog data;
  - unresolved-model and unsupported-reasoning warnings that omit only the
    unrepresentable field and never suppress the resource;
  - inheritance-versus-default omission semantics per resource surface;
  - source-native model spellings, floating family selectors with explicit
    catalog pins, and model-source provenance through ingestion and canonical
    archives, including stale-sidecar precedence and forge/malformed
    rejection;
  - source-revision v4 provenance fallback digests with byte-compatible
    v1/v2/v3 projection;
  - materialization that keeps warned resources and converges on the next
    run;
  - shipped-catalog validity enforced by the offline model-catalog check.
- No behavior changes; every requirement describes current code and is proven
  by an annotated existing test.

## Capabilities

### New Capabilities

- `harness-model-catalogs`: user-observable model and reasoning conversion
  for skills and subagents across harness targets — native spellings, loss
  diagnostics, provenance, and source-revision compatibility.

### Modified Capabilities

None.

## Impact

- OpenSpec artifacts only, plus `// spec:` annotations on existing tests.
- `docs/plans/2026-09-21-harness-model-catalogs-design.md`,
  `pkg/harness-compilation/README.md`, `pkg/harness-compilation/CHANGELOG.md`,
  and `pkg/harness-compilation/diag/codes.md` remain as descriptive
  references; the spec is normative where they disagree.
- No harvest-legacy-docs rows are absorbed by this change.
