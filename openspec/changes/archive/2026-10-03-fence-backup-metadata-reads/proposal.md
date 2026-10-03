## Why

Codex set discovery reads manifest aliases by path. A real two-partition backup followed by an external manifest alias authorizes restore and overwrites the current installed skill using foreign metadata claims.

## What Changes

- Enumerate Codex set manifests and archive-presence metadata through captured backup-directory authority.
- Read manifests through strict captured regular/single-link/nonblocking opened-file snapshots with captured-size/state checks, rejecting final aliases before claims are parsed.
- Preserve valid restore/list/rotation and optional missing/invalid-JSON behavior; payload readers and restore mutations remain next slices.

## Capabilities

### New Capabilities

- `backup-recovery`: captured backup metadata admission.

## Impact

R2 child of #498. Shared loader protects restore/list/rotation. No new arbitrary size or archive budget; archive creation/extraction/mapped publication, ownership state/journal/archive evidence remain open.
