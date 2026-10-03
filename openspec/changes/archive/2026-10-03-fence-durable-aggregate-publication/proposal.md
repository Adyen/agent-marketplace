## Why

Ownership Apply stages and publishes aggregates through separately reopened paths. Retargeting the parent after journal preparation writes private staged bytes outside the recorded root before CAS refusal. A same-byte replacement leaf passes digest CAS and is overwritten despite being a new generation.

## What Changes

- Capture authorized aggregate parent/destination before journal preparation and retain that authority through exclusive staging, mode selection and durable rename.
- Capture the staged generation and revalidate it before publication; refuse same-byte destination and staged-file replacements.
- Clean attempt-owned refused staging through captured directories while preserving injected-crash recovery evidence and foreign replacement stages.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: durable aggregate staging/publication authority.

## Impact

R2 child of #497. Existing crash ordering, fsync contract, backups, journal format, private creation and shared-mode updates retained. Recovery mutation/cleanup, state/journal and backup-manifest/payload consumers remain next layers; no new arbitrary budgets or writer-exclusion claims.
