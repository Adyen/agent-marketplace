## Why

SDK plans Removed before actual pruning, so conflict-preserved output and a stale file disappearing before rename both claim deletion. A real public sync with safe and refused/absent siblings reproduces false receipts despite unchanged unsafe bytes.

## What Changes

- Keep existing prospective dry-run Removed and complete StaleCandidates planning.
- Return a private actual-removal boolean from the existing mutation function; true only after successful validated quarantine unlink, retained independently of later close failure. Keep public RemoveStaleFileCandidate's error-only API unchanged.
- Build live SDK Removed from actual receipts, not discovery, while preserving sorted processing, original prune conflicts, healthy real cleanup and valid absence semantics.

## Capabilities

### Modified Capabilities

- `materialization`: SDK removal receipts distinguish actual mutation from planned/refused/absent output.

## Impact

R2 final known SDK follow-on, child of #562 in stack #560. Existing removal logic/safety/error behavior unchanged; private return contract and single SDK caller updated, one prior private test adapted without weakened assertions. No new helper/type/public API/dependency/budget/source-comment change. Final admitted-case appendix audit and integrated readiness remain open; does not close R2.
