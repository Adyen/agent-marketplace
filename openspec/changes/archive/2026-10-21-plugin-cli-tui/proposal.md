## Why

Plugin support (issue #6, v5.0.0) can declare, select, lock, and deliver
plugins as decomposed members (`plugin-sources`, `plugin-decomposed-delivery`),
but users cannot manage plugins from the CLI or TUI: `list --kind plugin` is
rejected, there is no way to inspect a plugin before installing it, status
does not separate locked and installed plugin revisions, and the TUI has no
plugin rows. A plugin that declares dependencies is rejected outright, so it
cannot even be inspected.

## What Changes

- `list --kind plugin` lists plugins.
- A new read-only `inspect plugin/<name>` command shows a plugin's authored
  name, exact source and revision, format, members, files, declared
  dependencies, unsupported content, per-harness-target delivery, losses,
  and blockers.
- `install`, `remove`, `update`, `sync`, `sync --check`, and `status` accept
  plugins. Status reports the locked revision and the installed revision
  separately, with the blocking diagnostic when an update is blocked.
- A plugin that declares dependencies stays inspectable; sync refuses it
  with `plugin.format.dependencies-unsupported` and installs none of its
  members.
- The TUI shows each plugin as one parent row with expandable member rows;
  toggling a member row edits that member's exclusion on the plugin request.
- A process-spawn guard proves plugin list, inspection, lock, sync, and
  status run no package process.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `plugin-packages`: plugins gain a CLI lifecycle, TUI parent rows, and the
  rule that plugin operations never execute package content.

## Impact

- `cmd/agent-marketplace`: `list`, new `inspect`, `install`, `remove`,
  `update`, `sync`, and `status` handling of plugins, with command witnesses.
- `internal/tui`: plugin parent rows and member exclusion edits; TUI QA
  journeys under `tools/tui-qa`.
- `pkg/harness-compilation`: plugin decoding records declared dependencies
  and the new `plugin.format.dependencies-unsupported` diagnostic.
- Lock and configuration formats are unchanged. Export, native delivery and
  registration, and documentation remain with later changes; the export
  path joins the process-spawn guard when `export` lands.
