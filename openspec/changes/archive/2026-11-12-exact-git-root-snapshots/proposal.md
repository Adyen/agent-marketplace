# Proposal

## Why

An exact snapshot is a checkout of the whole registry repository, and every command verifies every tracked file in it, even when the registry entry consumes one directory. A registry entry with `path: skills` in a large repository makes each `sync` hash thousands of files it never reads.

## What Changes

- A registry root configured with `path:` is acquired as its own exact snapshot that contains only the tracked files under that root. Verification covers exactly the files the command consumes.
- Two registry entries for the same source and commit with different paths each get their own snapshot.
- A registry root without `path:`, or with `path: .`, keeps the whole-repository snapshot.
- Whole-repository snapshots published by earlier releases keep working: read-only commands verify the configured root inside them, and the next mutating command publishes a root snapshot without user action.
- Acquisition locks, source revision digests, and diagnostics are unchanged.

## Capabilities

### New Capabilities

### Modified Capabilities
- `exact-snapshot-verification`: verification covers the consumed configured root; new requirement and scenarios for root-scoped snapshots, tampering inside and outside the root, multiple roots of one source, and reuse of existing whole-repository snapshots.

## Impact

Exact Git cache keying, publication, verification, and garbage-collection reachability, plus the commands that open exact snapshots (`sync`, `install`, `update`, `remove`, lock verification, plugin delivery). No configuration, acquisition-lock, API, or output format change.

## Release note

`sync` and other commands that read a Git registry with a configured `path:` now check out and verify only that directory, which makes them faster on large repositories. Existing caches keep working without any action.
