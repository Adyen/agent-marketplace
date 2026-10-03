## Why

State and phase-journal publication uses path-based temporary creation and rename. Retargeting the record directory after aggregate publication overwrites foreign state/journal metadata and later deletes the foreign journal.

## What Changes

- Reuse the shared durable descriptor publisher for state and phase-journal metadata beneath the recorded canonical state namespace.
- Capture strict existing record parents before staging; commit no longer recreates a missing parent.
- Create private exclusive staged members, verify the produced opened generation and named parent, and clean only tracked attempt-created staged inodes through captured directories.
- Keep private metadata modes and current public prepared-directory, JSON and crash semantics.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: metadata publication namespace authority.

## Impact

R2 child of #512. All public callers prepare record directories before metadata writing; legacy safety metadata atomicWrite remains a follow-on. Prior loaded-generation/lease binding, namespace preparation, journal/recovery cleanup, safety evidence and remaining backup cases remain open. No whole-mutation transaction or new budget/writer-exclusion claim.
