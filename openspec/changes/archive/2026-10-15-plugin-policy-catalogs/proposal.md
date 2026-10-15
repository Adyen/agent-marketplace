## Why

Plugin support (issue #6, v5.0.0) needs harness and package-format knowledge
that a maintainer can change with a catalog edit and evidence, the way PR #451
did for models, tools, and hooks. This change lands that knowledge first, with
no user-visible plugin behavior yet, and clears two existing plugin-layout
leftovers before plugins arrive: the Claude `.claude-plugin/plugin.json`
aggregate that no harness activates, and the Factory plugin form that silently
drops a root `SKILL.md` (#340).

## What Changes

- A fourth catalog family, `pluginpolicy`, ships one embedded catalog per
  recognized package format and one per registered harness target. The
  offline catalog check validates them strictly. Native delivery is declared
  verified only with an identifier of a native probe present in the harness
  validation suite; every harness stays unverified in this change.
- A validated package layout declares per-format manifest paths, component
  directories, and catalog root rules beside the native layouts.
- `make catalog-docs` generates a public plugin support reference from the
  catalogs, and `make catalog-check` fails when it is stale.
- **BREAKING** (output only): Claude Code skills and subagents no longer add
  entries to `.claude-plugin/plugin.json` under the Claude target root. Sync
  removes entries earlier versions wrote there and deletes the file when
  nothing else remains. The Factory Droid layout stops claiming
  `.factory-plugin`.
- A Factory plugin-form source with a root `SKILL.md` gets a warning instead
  of a silent drop.
- The domain glossary gains the plugin terms.

## Capabilities

### New Capabilities

- `plugin-policy-catalogs`: shipped, validated per-harness and per-format
  plugin catalogs, their offline check, and the generated support reference.

### Modified Capabilities

- `materialization`: Claude Code resources produce no plugin manifest
  entries, and earlier managed entries are retired.
- `harness-compilation`: a Factory plugin with a root skill is not dropped
  silently.

## Impact

- `pkg/harness-compilation`: new `pluginpolicy` package and catalogs,
  `target/package_layout.go`, Claude and Factory native layouts, the Claude
  compiler, the Factory ingester, and the diagnostic catalog.
- `internal/harness` and `internal/materialize` aggregate handling for Claude.
- `tools/catalog-docs`, `tools/harness-compat`, `tools/model-catalog-audit`,
  and the `Makefile` catalog targets.
- Public docs: a generated plugin support reference and pages that describe
  `.claude/.claude-plugin/plugin.json`.
- Out of scope: plugin IR, decoders, projection, and the plugin conversion
  fixtures (agent-marketplace-nvlx.2); configuration, locks, CLI, and TUI.
