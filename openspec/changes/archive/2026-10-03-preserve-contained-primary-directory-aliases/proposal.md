## Why

Disk-backed catalog ingestion currently rejects contained directory aliases and payload paths beneath aliased ancestors. Captured source hardening must retain native source layout compatibility without importing foreign bytes or weakening regular-file snapshot checks.

## What Changes

- Open contained source directory aliases as captured directories and read payloads below contained aliased ancestors.
- Keep source scope containment, regular singly-linked payload snapshots, captured-size growth checks, and typed publication refusal.
- Distinguish missing optional descendants beneath a valid alias from dangling alias targets.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `registry-symlink-containment`: clarify disk primary directory alias compatibility and refusal behavior.

## Impact

Captured disk source reads and native Factory catalog ingestion. Existing destination alias policy and configuration formats remain unchanged.
