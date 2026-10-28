## Why

The relocated Claude cleanup boundary must not change legacy cleanup of unrelated
user-owned JSON. Git administrative paths also need an explicit exclusion: an
absolute configuration directory is not permission to write repository metadata.

## What Changes

- Keep captured read/write authority exclusive to declared relocated Claude
  aggregates; preserve legacy recursive cleanup behavior for other files.
- Reject relocated roots in literal `.git`, configured Git directories, and
  common or linked-worktree metadata, including physical aliases.
- Share the existing relative-value predicate without changing its semantics.
- Skip unused non-directory component locations so independent cleanup proceeds.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `claude-native-configuration`: preserve legacy cleanup compatibility and
  exclude Git administrative destinations.
- `materialization`: unused non-directory components do not block cleanup.

## Impact

- Harness destination admission and materialization cleanup, with regression tests.
- No consumer, lock, artifact, ownership-state or transaction format changes.

## Release note

Relocated Claude output refuses Git administrative paths. Legacy synchronization
continues to ignore unrelated unmarked hard-linked JSON instead of failing it.
