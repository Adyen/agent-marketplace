## Why

Shared Gemini settings reconciliation adds a machine-scope output even when only repository resources are selected. Running ordinary artifact pruning for that extra output deletes a command written by the repository pass.

## What Changes

- Restrict an extra machine output used only for shared native settings to native reconciliation.
- Preserve selected repository commands through first synchronization, repeated synchronization, and check-only inspection.
- Retain ordinary cleanup when a shared root has no remaining desired resources.

## Capabilities

### New Capabilities

### Modified Capabilities

- `owned-entry-reconciliation`: shared Gemini native reconciliation does not introduce ordinary artifact pruning.

## Impact

Shared-root Gemini materialization only. Configuration loading, distinct roots, resource selection, and wire formats remain unchanged. The regression uses `materialize.Run`; normal CLI loading does not read repository configuration at HOME.
