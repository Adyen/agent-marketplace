## Why

With a missing optional selection, a TUI sync repeated the snapshot's "was selected but not found" warning as an operation warning. The footer showed `⚠ 2 warnings`, and the warnings screen listed the same text twice. After the user removed the selection and reloaded, the snapshot warning cleared but the sync's copy stayed until the next sync (#858). Decision on #858: an accepted snapshot retires duplicate operation copies.

## What Changes

- The visible errors and warnings hide an operation warning whose stable key matches a current snapshot warning.
- Replacing the snapshot retires operation warnings that repeated a warning of the previous snapshot which the new snapshot no longer reports. Operation warnings that no snapshot reported keep their existing lifecycle.
- `tui-error-recovery` adds "Operation copies of snapshot warnings follow the snapshot".

## Capabilities

### New Capabilities

### Modified Capabilities
- `tui-error-recovery`: operation copies of snapshot warnings follow the snapshot.

## Impact

`internal/tui` diagnostic store and visible diagnostic views. Errors keep their lifecycle unchanged.
