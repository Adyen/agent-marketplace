# Plugins and package formats

A plugin is a package resource: it has one identity and contains members such
as skills, commands, subagents, hooks, and MCP servers. The package is the
unit you select and install; its members are addressed by `kind/name` only
within that plugin. This differs from a [collection](../glossary.md#collection),
which groups resources but does not install as a package of its own.

## Package formats and native plugin catalogs

The `format` on a Git or directory source opts into plugin interpretation.
Without it, a source that happens to contain a plugin manifest keeps its
existing loose-resource behavior. A package format defines the manifest and
directory layout for one plugin, such as `agent-plugin`, `claude-plugin`,
`factory-plugin`, or `codex-plugin`.

A native plugin catalog is different: it lists plugins and where to acquire
them. Catalog formats such as `claude-marketplace`, `factory-marketplace`,
and `codex-marketplace` resolve each entry to its exact package source.
Relative entries are resolved from the catalog root. Catalog entries that
use a refused source type remain inspectable but cannot be installed, and a
catalog listing does not override the repository's source policy.

Plugin interpretation is opt-in per source. In a format source, select
`plugin/<name>` (or use `{kind: plugin, name: ...}` when the name cannot fit
the locator grammar). Do not select a member such as `skill/review` directly
from that source. `exclude` names exact members within the selected plugin;
unknown member locators fail instead of silently matching nothing.

## Decomposed and native delivery

With decomposed delivery, members are compiled as ordinary harness resources
under the deterministic name `<plugin>-<member>`. If that name collides or
is invalid for the target, the plugin instance is refused rather than
renamed with a suffix.

Native delivery keeps the complete package and registers it through
Agent-Marketplace-owned entries in the harness's native settings. The
catalogs declare native delivery verified only when a native probe provides
evidence for that harness and format. Agent Marketplace does not write
vendor trust state or private plugin caches. The generated
[plugin support reference](../reference/plugin-support.md) links to the
catalog-backed matrix rather than duplicating it here.

`auto` selects native only where the catalog marks it verified and uses
decomposed delivery otherwise. Native delivery is currently unverified for
all seven harnesses, so `auto` chooses decomposition for every selected
plugin instance. An explicit `delivery: native` request fails with
`plugin.delivery.native-unverified`; it never falls back to decomposition.

## Package generations and locks

Each installed plugin instance binds one plugin to a package generation, a
harness target, a delivery mode, and a scope. A package generation is an
immutable, complete tree published atomically under Agent Marketplace's
state root. Plugin-root files resolve from that generation, not from an
acquisition cache or an upstream checkout, so removing a disposable cache
does not break the installed package. Garbage collection can remove a
generation only after no installed projection, registration, or recovery
journal refers to it.

Git-backed plugin sources use the existing `agent-marketplace-lock/v2`
acquisition-lock format. Their source-revision inventory uses `plugin-v1`
and records the catalog and selected entry, exact catalog and plugin origins
and commits, the entry overlay, the package digest and file inventory, its
format and version, and the selection edges. Locked and offline operations
rely on those records instead of resolving a different catalog entry or
package. Directory sources remain live and are not pinned in an acquisition
lock. The [lockfiles reference](../reference/lockfiles.md) describes the
lock fields and how Git source records work.

## Why plugins are not executed

Discovery, inspection, locking, compilation, sync, export, and status treat
plugin contents as data. Agent Marketplace does not run package scripts,
acquisition commands, vendor CLIs, schema downloads, or plugin subprocesses
(Git acquisition is separate). A declared plugin dependency can be inspected,
but installation is refused with
`plugin.format.dependencies-unsupported`; dependency resolution is not
performed.

## Related documentation

- [Install a plugin](../how-to/install-plugins.md): select, inspect, export,
  sync, and remove a plugin.
- [Repository configuration](../reference/configuration.md): source formats,
  plugin locators, and request options.
- [Command-line reference](../reference/cli.md): plugin commands and flags.
- [Lockfiles](../reference/lockfiles.md): acquisition-lock structure.
- [Diagnostics](../reference/diagnostics.md): how to look up diagnostic
  codes.
