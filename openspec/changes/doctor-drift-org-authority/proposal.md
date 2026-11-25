# Proposal

## Why

On PR #1097, doctor can diagnose harness drift in an online managed installation without a deployed organization acquisition lock, then fail after confirmation because the apply step uses strict `sync --locked`. The failure also stops later ownership repairs.

## What Changes

- Allow doctor to reconcile harness drift with the same verified transient organization evidence admitted during diagnosis, without writing or refreshing any acquisition lock.
- Keep standalone `sync --locked` strict: it still refuses an absent organization acquisition lock when one is required.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `doctor-repair-planning`: Make diagnosis and execution agree on acquisition authority for harness drift.

## Impact

Doctor's CLI drift step and its internal sync invocation; no new CLI flags, organization file writes, or changes to TUI behavior.
