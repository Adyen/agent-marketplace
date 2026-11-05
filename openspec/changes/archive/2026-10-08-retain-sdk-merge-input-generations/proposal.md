## Why

Marker-based aggregate publication can replace a concurrent native edit with stale merged bytes. Existing JSON is read before the later comparison captures publication authority, so a replacement between those two reads becomes the newly admitted destination even though it did not supply the merge input. A public Factory native MCP regression reproduces this loss with a successful sync.

## What Changes

- Read SDK marker-merge input through the existing captured regular-file boundary rather than an unguarded pathname read.
- Carry the input's full generation or genuine absence into comparison and existing publication revalidation; never rebase stale merge output onto a newer destination.
- Preserve concurrent bytes and metadata on typed generation refusal, including read-only planning and byte-identical replacements.
- Preserve valid native updates, shared permissions, JSONC formatting, no-ops, private creation and contained native alias migration.

## Capabilities

### Modified Capabilities

- `materialization`: marker-merge publication retains the generation that supplied its input.

## Impact

Shared SDK reader/comparison and private merge-preparation wiring, actual native and SDK regressions, and a disk-I/O benchmark. No public API, manifest format, new dependency, lock/retry policy, processing ceiling or whole-operation transaction. Existing root admission and accepted quarantine boundaries remain unchanged. This addresses the demonstrated concurrent-aggregate case, not all C2 or R2 claims.
