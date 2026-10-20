## Why

Plugin support (issue #6, v5.0.0) can now declare, select, and lock plugins
(`plugin-sources`), but sync skips their harness delivery with a
`plugin.delivery.pending` warning. Plugins need to reach every harness
target as decomposed members whose plugin-root references point at an
immutable package tree owned by Agent Marketplace, never at the acquisition
cache. The support-file assembly from the closed PR #273 is folded in here,
because package generations are its only remaining consumer now that the
`convert` subcommand is gone.

## What Changes

- Sync delivers plugin requests with `delivery: auto` or `decompose` as
  decomposed members to all seven harness targets, using the installed name
  `<plugin>-<member>` and refusing namespace collisions and invalid names.
  Requests with `delivery: native` keep the `plugin.delivery.pending`
  warning until native delivery lands; they are never decomposed.
- Each plugin instance (one plugin, one harness target, one scope) installs
  as a unit through one dependency transaction participant: a failing member
  blocks the instance and keeps its previous projection.
- Package generations are published atomically under the state root, tracked
  in ownership state, retained while referenced, and collected by `gc` when
  unreferenced. Plugin-root references resolve to the generation.
- Persistent plugin data lives outside generations and caches, survives
  update and removal, and removal reports the retained path.
- Delivered members follow the post-exclusion member keys committed per
  layer in the plugin-v1 lock manifests.
- The harness-compilation library assembles complete support bundles: skill
  support files and plugin-root-referenced hook assets keep exact bytes and
  modes, escaping symlinks fail closed with `bundle.support-file.unsafe`,
  and output never embeds an author-machine source path.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `plugin-delivery`: plugin instances install as a unit, decomposed members
  use a deterministic namespace, package roots resolve to immutable package
  generations, and removal retires only owned plugin output.

## Impact

- `pkg/harness-compilation`: support-file assembly, diagnostics, changelog,
  and field matrix.
- `internal/pipeline`, `internal/materialize`, `internal/ownership`,
  `internal/dependencytxn`: package generations, decomposed member delivery,
  instance participants, removal, and plugin-data retention.
- `cmd/agent-marketplace`: sync output and `gc` collection of generations.
- `e2e`: crash-recovery coverage for interrupted plugin delivery.
- Plugin-free configurations, locks, and output keep their bytes. Native
  delivery, registration, delivery transitions, plugin CLI paths, TUI rows,
  and export remain with later changes.
