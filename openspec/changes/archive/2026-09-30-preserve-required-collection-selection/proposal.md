## Why

Repository collection selection can disable automatic selection from an otherwise unqualified required organization registry. An unrelated repository request must not narrow organization-required resources.

## What Changes

- Retain automatic required selection during cross-registry top-level collection lookup.
- Preserve an explicitly authored required subset and existing collection lookup diagnostics.
- Prove resolver selection and CLI materialization with inert resources.

## Capabilities

### New Capabilities

### Modified Capabilities

- `resource-selection`: top-level collection lookup cannot narrow automatic required selection.

## Impact

The shared resolver changes for legacy selectors and compatible runtime projections. Configuration and acquisition-lock formats remain unchanged. Explicit required subsets, optional/default selection and collection cycles retain their existing contracts.
