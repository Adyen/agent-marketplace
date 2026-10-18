## Why

Plugin support (issue #6, v5.0.0) needs consumers to opt a source into
package interpretation, select plugins as resources, read native plugin
catalogs, and lock plugin acquisition exactly. `plugin-package-ir` added the
plugin IR, package decoders, and projection in the harness-compilation
library, but no configuration, selection, or lock reaches them yet. This
change wires sources, selection, catalogs, and locks, with harness delivery
left to `plugin-decomposed-delivery`.

## What Changes

- Git and directory sources accept an optional `format` naming a package
  format (`agent-plugin`, `claude-plugin`, `factory-plugin`,
  `codex-plugin`) or a native plugin catalog format (`claude-marketplace`,
  `factory-marketplace`, `codex-marketplace`). Sources without `format`
  keep their existing ingestion.
- Plugins are selectable with `plugin/<name>` or `{kind: plugin, name:
  <name>}`, with per-request `exclude` and `delivery` options. Collections
  can select a plugin, which stays one plugin. Members of a format source
  cannot be selected directly, and exclusions must name exact members.
- Native plugin catalog decoders for `claude-marketplace`,
  `factory-marketplace`, and `codex-marketplace` resolve each entry to an
  exact source, refuse unsupported entry source types and duplicate names
  per entry, and check every origin against source policy on its own.
- Plugin acquisition is locked in the existing `agent-marketplace-lock/v2`
  format: format roots use a new `plugin-v1` source-revision contract whose
  digest commits to the catalog index, selected entries and their overlays,
  format and version, and selection edges; plugins are `kind: plugin`
  resource records with their package digest and file inventory; external
  catalog entries get their own exact Git records. Layers without plugins
  keep their lock bytes.
- Until plugin delivery lands, sync locks selected plugins and skips their
  harness delivery with a `plugin.delivery.pending` warning.
- The released-client gate proves that released v4 clients refuse a plugin
  configuration before any write.

## Capabilities

### New Capabilities

- `plugin-packages`: format opt-in, plugin selection, member identity and
  exclusion, native plugin catalog resolution, exact plugin locking, and
  released-client fail-closed behavior.

### Modified Capabilities

None.

## Impact

- `internal/config`: v4 source `format`, plugin locators, the structured
  entry, and plugin request options, including editor round-trips.
- `internal/catalog`, `internal/resolver`: format-source discovery, plugin
  selection edges, collection plugin members, and refusals.
- `pkg/harness-compilation/plugindecode`: native plugin catalog decoders.
- `internal/lockfile`, `internal/sourcerevision`: the `plugin-v1`
  source-revision contract and plugin records inside v2 locks.
- `e2e`: the released-client gate scenario.
- No lock `apiVersion` change, no new lock fields, and no change to
  configuration, locks, source revisions, or output without plugins.
- Out of scope: harness delivery of plugin members, package generations,
  plugin CLI inspection and TUI rows, export, and native registration.
