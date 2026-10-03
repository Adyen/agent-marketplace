## Why

Durable ownership state and journal readers use os.ReadFile. A leaf alias can supply matching ownership claims to Proof or authorize recovery against a journal borrowed from outside the state namespace; special-file opens can block.

## What Changes

- Record the canonical selected state-root spelling at Store.Open without creating files, preserving initial explicitly selected state-root aliases and public StateRoot spelling.
- Share a captured metadata reader beneath strict canonical-root/ownership-key descriptors with regular/single-link/nonblocking/captured-size opened and named-state validation.
- Normalize genuine optional absence inside the reader before deferred cleanup; propagate cleanup and unsafe errors without reducing them to missing state/no journal.
- Preserve state-corrupt coded diagnostics and filesystem error identity, proof fail-closed behavior, JSON format and valid recovery.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: durable ownership metadata read authority.

## Impact

R2 child of #511. Reader only: durable state/journal publication, recovery cleanup, safety backup evidence and remaining backup namespace/restore generations remain follow-ons. No new byte/work limit, transaction or durability promise.
