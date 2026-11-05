## Why

Nightly documentation fingerprint changes can create pull requests that update
only evidence hashes and checked dates, without changing any shipped catalog
value. Those updates create review noise and can acknowledge unreviewed evidence.

## What Changes

- Propose catalog additions without automatically updating evidence baselines.
- Keep evidence-only drift and manual candidates visible in audit artifacts and
  job summaries.
- Publish automated proposals only when model catalog files change.

## Capabilities

### New Capabilities

- `model-catalog-audit`: observable file-write and publication policy for nightly
  catalog proposals.

### Modified Capabilities

None.

## Impact

Changes the internal audit proposal tool, nightly workflow, contributor guidance,
and offline regression tests. The shipped CLI and harness translation behavior
are unchanged.
