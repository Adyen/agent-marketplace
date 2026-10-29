## Why

The nightly proposal contains seven new model mappings without reviewed
conversion fixtures or refreshed evidence. Adding Factory's GPT-6 Sol Fast also
invalidates a fixture that previously expected that distinct identity to be
unavailable.

## What Changes

- Verify the proposed Codex and Factory model IDs against their first-party
  model inventories and subagent field documentation.
- Add documented reasoning support for Codex GPT-6.1 Terra and monitor its
  first-party model page.
- Prove every new mapping, preserve fast-model identities in both directions
  for explicitly supported reasoning levels, and retain warnings for
  unsupported destinations and reasoning levels.
- Manually refresh only the reviewed Codex and Factory inventory baselines;
  keep unrelated evidence drift and manual-entry candidates visible.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `harness-model-catalogs`: reviewed Codex and Factory model additions and their
  native reasoning limits.

## Impact

Changes shipped model catalog data, conversion coverage, monitored evidence,
and contributor guidance. No resolver, serializer, configuration, acquisition
lock, or automatic evidence-acknowledgement policy changes.

## Release note

Subagent conversion supports Codex GPT-6.1 Terra and six additional Factory
models, including GPT-6 fast variants. Devin/Factory fast-model conversions
retain their distinct identity for explicitly supported reasoning levels.
Factory's `gpt-6-sol-fast` without a reasoning level is omitted when converting
to Devin, with a warning. Unsupported reasoning levels still produce omission
warnings.
