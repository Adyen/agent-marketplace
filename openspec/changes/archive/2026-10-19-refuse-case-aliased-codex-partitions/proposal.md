## Why

Codex destination preflight decides whether repository and machine scope share a native partition by comparing path spellings. On a case-insensitive filesystem, setting `CODEX_HOME` to `<repo>/.CODEX` names the repository's own `.codex` directory, but preflight saw two distinct partitions. It neither coalesced their desired state nor refused conflicting writes, so one scope silently replaced the other's output: a repository and a machine subagent with the same name left only the machine definition, with no diagnostic. With the exact spelling `CODEX_HOME=<repo>/.codex` the same selection is refused with `materialize.destination.overlap`.

## What Changes

- Codex managed paths in different partitions are compared case-insensitively, on every host, and refused with `materialize.destination.overlap` when they are equal or one contains the other. The diagnostic names both spellings.
- Managed paths that differ only in Unicode normalization are refused the same way, whether or not the directory exists yet.
- Partition roots that are the same existing directory under other spellings (for example macOS firmlinks) are compared as one location.
- Disjoint partitions whose roots differ only in letter case, and partitions with identical spellings, behave as before.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `materialization`: Codex partitions that name one location under different spellings are refused before mutation.

## Impact

Codex destination preflight only. Configurations that spell `CODEX_HOME` as a case variant of a repository `.codex` directory are now refused on every host, including case-sensitive hosts where the two spellings are separate directories, matching the portable rule for case-only artifact path collisions. No configuration, lock or ownership format changes.
