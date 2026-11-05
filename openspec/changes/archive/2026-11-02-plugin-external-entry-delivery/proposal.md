## Why

A plugin selected from a native plugin catalog entry whose source is an
external Git repository is locked to its own exact commit, but sync never
delivers it: every harness target reports `plugin.delivery.pending`. Such
plugins need the same delivery as plugins that live inside the catalog
source.

## What Changes

- Sync acquires each selected external catalog entry at the commit its lock
  record names, decodes the package there with the owning catalog's entry
  policy, and delivers it through the existing delivery resolution:
  decomposed members `<plugin>-<member>` with an immutable package
  generation, or native delivery where the target's policy already allows
  it.
- Delivered members follow the post-exclusion member keys committed in the
  external record, so authored exclusions apply.
- A moved upstream ref changes nothing until the lock is updated; locked,
  check, and plain sync all deliver the recorded commit.
- An external entry whose locked commit cannot be opened, or that has no
  locked record, blocks only that plugin instance with an error naming the
  plugin. It is never dropped silently and never read from the live ref.
- `plugin.delivery.pending` no longer applies to external Git entries.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `plugin-delivery`: external catalog entries deliver from their locked
  commit.

## Impact

- `internal/pipeline`: delivery of external catalog entries from locked
  snapshots.
- `cmd/agent-marketplace`: sync regressions for external entries.
- `pkg/harness-compilation/diag/codes.md`: the narrowed
  `plugin.delivery.pending` meaning.
- Lock format, external lock records, catalog resolution, native delivery
  policy, and native registration are unchanged.
