## Why

Declared-path cleanup must distinguish a genuinely absent Claude aggregate from
an inspection failure. Otherwise stale owned entries can remain while
synchronization reports success.

## What Changes

- Report non-absence inspection errors as file-scoped cleanup diagnostics.
- Continue processing safe sibling aggregates.
- Preserve absence behavior for missing files and non-directory components.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `claude-native-configuration`: make declared aggregate inspection failures visible.

## Impact

- The declared-path cleanup branch in materialization and its regression tests.
- No consumer, acquisition-lock, native artifact, ownership, or journal format changes.

## Release note

Synchronization now reports an error when a declared relocated Claude aggregate
cannot be inspected, instead of silently treating it as absent.
