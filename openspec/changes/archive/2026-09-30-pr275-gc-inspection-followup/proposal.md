## Why

An interrupted Marketplace cache write can block collection even when the remaining metadata identifies all live archives. Status also labels malformed resources as errors while its shared inspection result treats them as warnings. Both problems surfaced in review of the consolidated configuration-loading change.

## What Changes

- Ignore incomplete writer-owned cache residue when finding reachable artifacts, while retaining fail-closed handling for malformed completed metadata and bounded reads.
- Use the shared inspection classification for malformed resources in status, sync checks, and the terminal UI, with accurate source paths and exit status.
- Prove dry-run parity and lease-time input changes with focused regression tests.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cache-garbage-collection`: distinguish incomplete cache writes from malformed completed metadata, bound metadata reads, and prove lease-time reachability and dry-run parity.
- `inspection-outcomes`: classify malformed resources consistently across status, sync checks, and terminal UI.

## Impact

Marketplace cache traversal, shared inspection and status rendering, command and cache regression tests. No config schema, acquisition policy, or cache pruning expansion.
