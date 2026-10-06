## Why

SDK stale pruning reserves a private random quarantine leaf, then renames over that name without retaining the creator inode. Replacing the reservation lets public sync consume and delete a foreign payload. Refusal cleanup also unlinks the name without attempt identity.

## What Changes

- Capture the descriptor stat of the exclusively created quarantine reservation.
- Before rename require the original regular singly linked empty reservation; replacement or read refusal returns the existing stale-prune conflict.
- Cleanup removes only a matching reservation and propagates refusal/close errors, including creation failures with known attempt identity; unknown authority is preserved.
- Keep existing quarantine restore semantics and actual healthy stale pruning.

## Capabilities

### Modified Capabilities

- `materialization`: SDK quarantine reservation publication and attempt cleanup authority.

## Impact

R2 child of restacked #551 in stack #478. Existing shared descriptor matcher and private candidate type reused; no new helper/type/public API/dependency/budget/source-comment change. Final quarantined-payload deletion/restoration leases, removal reporting and empty-parent cleanup remain separately audited. No whole transaction or atomic uncooperative-writer exclusion.
