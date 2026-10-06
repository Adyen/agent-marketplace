## Why

Recovery cleanup checks journal generation only after roll-forward commits state or prepared cleanup consumes staged evidence. Recovery also drops the originally loaded record identity before state publication.

## What Changes

- Reuse the original metadata validator for real state and journal callers.
- Retain opened record identity/absence and journal generation through recovery phase dispatch.
- Recheck both before recovery actions and within existing validated record publication.
- Extend the existing optional Crasher with one read-to-action checkpoint for deterministic public recovery controls; production installs no crasher.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: recovery original metadata-to-action binding.

## Impact

R2 child of #528. No JSON/source-comment changes, dependency/budget/whole recovery transaction or uncooperative-writer exclusion. Native aggregate may already have published before the recorded crash; refusal preserves it. Final admitted-case audit and queued comments remain open.
