## Why

SDK verifies quarantined stale payload with a bounded descriptor reader, closes that reader, then deletes the quarantine name. Replacing the quarantined payload after verification deletes foreign bytes and reports success.

## What Changes

- Reuse the existing bounded/no-follow/nonblocking single-link candidate matcher at the final quarantined-payload removal boundary.
- Preserve unknown/replacement quarantine generations with the established stale-prune conflict, without restoring them into the original stale name or letting attempt cleanup remove them.
- Preserve real healthy pruning and creator-reservation/empty-parent generation controls.

## Capabilities

### Modified Capabilities

- `materialization`: final SDK quarantine removal remains bound to the admitted payload.

## Impact

R2 child of #559 above #558 in continuation stack #560. No new helper/type/public API/dependency/budget/source-comment change. One meaningful structured removal boundary, final existing matcher revalidation and failure return. No whole-operation transaction or atomic uncooperative namespace exclusion. Quarantine restoration and removal reporting remain separate dispositions before final admitted-case closure.
