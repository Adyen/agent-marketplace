## Why

Durable aggregate readers validate final leaves but reopen parent paths and read without captured size/generation authority. Actual Gemini root retarget after ownership-session acquisition accepts foreign bytes as a healthy no-op; a store-relative nested alias also imports an external payload.

## What Changes

- Introduce one real store-relative captured aggregate reader bound to the store's recorded canonical root, strict ancestor walking and regular/single-link/nonblocking captured-size reads.
- Route Gemini/Devin/Factory JSON reconciliation/repair, Codex reconciliation/repair, safety-backup reads, CAS and recovery live reads through it.
- Preserve typed unsafe/generation errors, genuine legacy absence identity, canonical root aliases and cleanup errors; refuse traversal and final payload aliases.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: canonical-root aggregate read authority.
- `materialization`: durable adapter reader refusal.

## Impact

R2 child of #495. Store publication/recovery mutation, backup manifests/payload consumers and durable ownership state/journal reads remain next layers. Unscoped ReadAggregateFile remains a compatibility/test utility with no production consumer after this port. No arbitrary new size limits, transaction or writer-exclusion claim.
