## Why

Same-harness conversion currently forwards separate reasoning fields without
validating known destination models. A Factory Nemotron subagent requesting
unsupported `low` reasoning therefore emits that value without the required
omission warning. The public `Convert` path reproduces the gap.

## What Changes

- Validate separate native reasoning fields for known destination models in
  the shared resolver while preserving native model spellings.
- Preserve supported native encodings, unknown destination models, and
  identities available only in another harness target.
- Use resolved family pins' capabilities without rewriting native selectors.
- Add public conversion regressions and a measured resolver benchmark.
- Clarify the legacy Codex warning catalog text to cover same-target omissions.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `harness-model-catalogs`: enforce reasoning omission for known same-harness
  selections while preserving native spellings and forward compatibility.

## Impact

Changes shared model resolution and conversion coverage. No public API,
artifact schema, diagnostic code, filesystem ownership, or catalog evidence
baseline changes.

## Release note

Same-harness subagent conversion now warns and omits unsupported reasoning
for known destination models. Native model spellings, supported reasoning,
and unknown-model preferences remain unchanged.
