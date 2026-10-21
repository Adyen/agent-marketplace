## Why

Plugin support (issue #6, v5.0.0) can select, lock, inspect, and deliver
plugins, but a locked plugin cannot be written back out as a package. Users
who want to share a plugin with a harness or catalog that reads Agent Plugins
1.0.0 have no way to produce one, and nothing tells them what such a package
would lose.

## What Changes

- A new `export plugin/<name> --format <format> --out <dir>` command writes a
  locked plugin as an Agent Plugins 1.0.0 (`agent-plugin`) package into a new
  or empty directory.
- Export is loss-intolerant by default: any member, field, or file the format
  cannot represent fails the export with `plugin.export.loss`, lists the
  losses, and writes nothing.
- `--allow-loss` omits and lists representable-loss content, but never drops a
  security restriction.
- `agent-plugin` is the only supported export format in this change; other
  formats are refused before anything is written.
- The export path joins the plugin process-spawn guard.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `plugin-portable-format`: Agent Plugins 1.0.0 export semantics.

## Impact

- `cmd/agent-marketplace`: new `export` command with command witnesses, and
  the export path in the plugin process-spawn guard.
- `pkg/harness-compilation`: a pure Agent Plugins exporter and new
  `plugin.export.*` diagnostics.
- Lock and configuration formats are unchanged. `convert` is not used or
  changed. Native package export, documentation, and release notes remain
  with later changes.
