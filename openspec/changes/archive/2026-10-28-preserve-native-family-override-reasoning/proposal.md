## Why

A native family override can preserve supported reasoning during recursive
resolution, only for the final override guard to discard it because family
IDs are absent from the native-model index. Override finalization should
respect validated family capabilities rather than contradict them.

## What Changes

- Preserve validated reasoning for declared family overrides on surfaces with
  a separate effort field.
- Retain omission for unsupported family reasoning, unknown overrides, and
  model-encoded family overrides.
- Prove public compilation behavior and compare override benchmarks.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `harness-model-catalogs`: keep supported native reasoning on explicit
  destination family overrides.

## Impact

Changes the shared resolver's final override guard and adds public compiler
regressions and benchmarks. No API, schema, diagnostic-code, or catalog-data
changes.

## Release note

Explicit model overrides selecting a declared catalog family now retain
supported native reasoning on separate-effort surfaces. Unsupported family
reasoning and unknown-override omission behavior remain unchanged.
