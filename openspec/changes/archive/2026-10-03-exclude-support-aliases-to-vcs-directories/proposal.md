## Why

Accepting contained support directory aliases can expose excluded `.git` content under an alias with a different logical name. Real copying and read-only checking must preserve the Git-internals exclusion after resolving a directory alias.

## What Changes

- Exclude support directory aliases whose contained resolved target is `.git` or one of its descendants.
- Keep ordinary contained aliases, executable payload snapshots, and both materialization modes unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `materialization`: make directory alias targets obey the Git-internals exclusion.

## Impact

Shared support directory classification and its real-copy and virtual-digest consumers.
