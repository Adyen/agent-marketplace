## Why

SDK stale-file digests admitted multiply linked files, allowing public sync to delete them and report success. Quarantine verification opened a replaced FIFO in blocking mode. Both readers hashed through unconstrained payload paths or descriptors without captured-size/full-state checks.

## What Changes

- Reuse captured directory no-follow opens and the bounded descriptor reader for original stale-candidate digests and revalidation, matching original full state and rejecting leaf aliases.
- Open quarantine payloads nonblocking, validate singly linked regular shape, and reuse bounded descriptor reader with captured size/full-state checks.
- Preserve quarantine restoration and existing healthy prune/report behavior; propagate reader close errors.

## Capabilities

### Modified Capabilities

- `materialization`: SDK stale payload read admission.

## Impact

R2 child of #548 in stack #478. Shared SDK digest callers include public Materialize and CLI conversion stale revalidation/removal. Existing fsboundary APIs; no new helper/type/public API/dependency/absolute budget/source-comment change. Directory discovery/removal generation boundaries remain separate; no whole transaction or atomic uncooperative-writer exclusion.
