## Why

The native reasoning guard recognizes family pins only if another catalog
also declares the family identity as a concrete model. Factory's shipped
`anthropic/sonnet` therefore forwards unsupported `max` reasoning without a
warning. The synthetic test accidentally provides the unrelated concrete
record and masks the missing recognition step.

## What Changes

- Recognize exact local family IDs for same-harness separate-effort surfaces.
- Reuse the existing pinned native-encoding validation without rewriting
  authored model strings.
- Preserve cross-harness inference and undeclared native selectors.
- Add a shipped-catalog public conversion regression, remove the masking
  fixture, and compare resolver performance.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `harness-model-catalogs`: recognize native catalog family IDs locally before
  checking their reasoning capabilities.

## Impact

Changes shared model selection and regression coverage. No API, schema,
diagnostic code, catalog data, or reviewed evidence-baseline changes.

## Release note

Same-harness subagent conversion now validates declared catalog family
selectors against their native reasoning capabilities without requiring
another harness's concrete model entry. Native spelling, unknown selectors,
and cross-harness inference remain unchanged.
