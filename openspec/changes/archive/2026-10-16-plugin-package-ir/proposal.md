## Why

Plugin support (issue #6, v5.0.0) needs a plugin to exist as a resource in
the harness-compilation library before sources, locks, or delivery can use
it. The `pluginpolicy` catalogs from `plugin-policy-catalogs` describe
package formats and harness plugin surfaces, but nothing yet decodes a
package or projects it to a harness target through those catalogs. This
change adds that library layer, with no consumer wiring, so later slices
build on one decoder and one projection.

## What Changes

- `ir.KindPlugin` is appended after `ir.KindRule` without renumbering. A
  plugin carries its format and version, manifest metadata, ordered members
  keyed `kind/name` holding typed leaf IR, a contained file set, and inert
  extensions plus records of invalid or unsupported content. Plugin names
  follow their format's grammar, separate from `ir.ValidateName`.
- Package decoders for `claude-plugin`, `factory-plugin`, `codex-plugin`,
  and `agent-plugin`. The Agent Plugins 1.0.0 decoder uses bundled schema
  data and never fetches `$schema`. Decoders report source locations,
  detect duplicate keys, bound reads, and reject escaping paths, cycles,
  special files, and case-fold collisions.
- A pure `pluginpolicy.Project` lowers members through the existing leaf
  compilers and model/tool/hook resolvers, applies the harness catalog's
  plugin-surface field policies, and returns a projection plus losses. A
  public package compile entry point exposes it; `Compile` given a plugin
  returns `plugin.compile.package-required`.
- The Claude Code harness catalog blocks a plugin subagent's
  `permissionMode` on the plugin surface.
- Hand-written plugin conversion fixtures run through the package compile
  path in `make catalog-check`.
- The library charter in `pkg/harness-compilation/AGENTS.md` stops calling a
  plugin "just a bundle", and an ADR records the decision.

## Capabilities

### New Capabilities

- `plugin-portable-format`: Agent Plugins 1.0.0 import rules and portable
  MCP runtime values.
- `plugin-delivery`: security restrictions on the plugin surface are never
  accepted as loss.

### Modified Capabilities

- `plugin-policy-catalogs`: conversion fixtures prove plugin catalog
  policies.

## Impact

- `pkg/harness-compilation`: `ir`, a plugin decoder package, `pluginpolicy`
  (projection, conversion fixtures, Claude catalog surface entry), the
  public package compile entry point, the diagnostic catalog, and
  `AGENTS.md`.
- `docs/adr/`: a new ADR on plugins as package resources.
- No change to configuration, locks, CLI, TUI, materialization, or output
  for existing resources.
- Out of scope: native plugin catalog decoders, configuration and
  selection, locks, package generations, native registration, export, and
  the plugin support reference.
