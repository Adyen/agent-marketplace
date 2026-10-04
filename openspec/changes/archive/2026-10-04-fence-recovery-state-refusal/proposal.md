## Why

Recover ignores state-load errors except root mismatch. A captured reader refusal on aliased state still lets a valid prepared journal remove its stage/journal, consuming recovery evidence despite the failed metadata boundary.

## What Changes

- Mark actual metadata read/admission/cleanup errors privately after genuine absence normalization, preserving filesystem causes and public diagnostics.
- Propagate those read refusals before recovery phase decisions/cleanup.
- Preserve ordinary malformed-JSON/missing-state recovery behavior.

## Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: recovery state refusal propagation.

## Impact

R2 child of #525. Actual interrupted prepared-generation witness/real rollback control. Does not establish original journal/state action leases or a whole recovery transaction/new budget/exclusion. Those and backup/final audit remain open.
