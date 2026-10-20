## Context

This change is the CLI and TUI slice of the draft `plugin-packages` change
(draft tasks 6.1, 6.2, and 6.4). Selection, locks, and decomposed delivery
already exist from `plugin-sources` and `plugin-decomposed-delivery`.

## Goals / Non-Goals

**Goals:** plugins are listable, inspectable, installable, removable,
updatable, checkable, and visible in status and the TUI; dependency-declaring
plugins are inspectable and refused at sync; plugin operations start no
package process.

**Non-Goals:** `export`, native registration and delivery, documentation,
lock or configuration format changes.

## Decisions

### D1: Inspection is a new read-only `inspect` command

`inspect plugin/<name>` takes exactly one plugin locator and writes nothing.
It reuses the resolver and package compilation used by `list` and `sync`
in inspect mode, so its report matches what sync would deliver. A
non-plugin locator or unknown plugin exits non-zero. `export` can later
reuse the same report.

### D2: Dependencies are decoded, then refused at delivery

The decoder records a manifest's declared dependencies on the plugin IR
instead of rejecting the package. Inspection shows them. Sync refuses the
plugin instance with `plugin.format.dependencies-unsupported` before any
member is staged, so none of its members install and siblings continue.

### D3: TUI plugin rows reuse the existing tree rows

A plugin renders as a parent row whose children are its members. Member
rows are not selectable resources: toggling one edits the `exclude` list of
the plugin request through the v4 editor and never writes a leaf request.

### D4: Process-spawn guard

A test seam records every subprocess start. The guard runs list, inspect,
lock, sync, and status over a plugin whose manifest and members reference
executable scripts and asserts no start other than Git acquisition. The
`export` path is added to the same guard when it lands.

## Risks / Trade-offs

- The scenario "Plugin lifecycle runs no package process" names export,
  which does not exist yet; this change proves every other named operation
  and leaves export to the change that adds it.
