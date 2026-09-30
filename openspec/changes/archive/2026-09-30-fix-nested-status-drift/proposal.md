## Why

`status` previously reported drift for a nested personal-local configuration even after `sync` reconciled its output, listing false writes and unrelated parent-root removals. The upstream fix is merged; this change pins the parity contract in the status-reporting spec and adds the regression coverage that was missing.

## What Changes

- Specify truthful drift and orphan reporting when a nested personal-local configuration determines the output root (behavior already implemented by workspace inspection).
- Add command-level regression tests for in-sync and drifted nested output, including unrelated parent-owned artifacts.
- Change no production behavior; preserve root-level inspection, read-only status, and exact-lock error reporting.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `status-reporting`: Define truthful drift and orphan reporting when a nested personal-local configuration determines the output root.

## Impact

The `status` command tests and the status-reporting specification change. Production code, synchronization, and ownership policies do not change.
