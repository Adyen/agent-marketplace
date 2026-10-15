## Context

This is the first slice of the v5.0.0 plugin epic (issue #6). The full plan
lives in the unarchived `plugin-packages` draft; this change lands only its
policy catalogs (draft tasks 0.3, 0.4, 1.1–1.3, 1.5) and the task 1.3 and
#340 decisions. The plugin conversion fixtures (draft task 1.4) move to the
slice that adds the package compilation path, because they must run through
it.

PR #451 set the pattern reused here: small per-concern catalog families
(`modelpolicy`, `toolpolicy`, `hookpolicy`), one strictly decoded,
evidence-bearing YAML file per harness, topology kept in the validated Go
`NativeLayout`, `make catalog-check`, `make catalog-docs`, and a nightly
drift audit.

Today Claude Code skills and subagents also add a `skills`/`agents` entry to
`.claude-plugin/plugin.json` inside the Claude target root, merged with the
`_managed_by` marker. No harness reads that file from a target root, and the
entry fields already live in each resource's frontmatter. The Factory layout
claims a `.factory-plugin` component that the compiler never writes.

## Goals / Non-Goals

**Goals:**

- Make plugin harness knowledge a catalog edit backed by first-party
  evidence. Enabling native delivery later is a catalog change bound to a
  probe identifier.
- Remove the vestigial plugin-manifest output before plugin delivery reuses
  the name `.claude-plugin`.
- Stop dropping a Factory root `SKILL.md` silently.

**Non-Goals:**

- Plugin IR, decoders, projection, and plugin conversion fixtures.
- Configuration, locks, CLI, or TUI changes.
- Declaring native delivery verified for any harness.

## Decisions

### D1: `pluginpolicy` is a fourth catalog family; paths stay in Go layouts

```text
pkg/harness-compilation/
  pluginpolicy/
    formats/<format>.yaml        package and native plugin catalog formats
    catalogs/<harness>.yaml      one per harness target (all seven)
  target/package_layout.go       per-format manifest path, component
                                 directories, catalog root rule
```

Package formats are not harness targets: Agent Plugins has no runtime, and
Claude Code accepts both `claude-plugin` and `agent-plugin`. Formats and
harnesses therefore get separate files. The seven formats are
`claude-plugin`, `factory-plugin`, `codex-plugin`, `agent-plugin`,
`claude-marketplace`, `factory-marketplace`, and `codex-marketplace`. Paths
are topology, so they live in a validated Go package layout beside
`NativeLayout`, with the same checks (clean relative paths, element-boundary
disjointness, no partition-root claims). Plugin catalogs reference the other
families instead of copying them; the plugin-root token references
`hookpolicy.plugin_env`.

### D2: Catalog schemas

Keys are vendor spellings. Values are closed enums and allowlisted policy
IDs only: no expressions, templates, commands, or schema URLs to fetch.

**Format catalog** (`formats/<format>.yaml`): `schema`, `format`,
`evidence{checked, refs, notes}` (required), `role` (`package` or
`catalog`), `versions`, `manifest.fields.<name>` (`required`, `type`, and
invalid-value outcome `refuse-package` or `ignore-with-warning`),
`components.<kind>` (`immediate-children`, `file`, `inline-or-file`),
`catalog.sources.<type>` (`acquire` or `refuse`), `catalog.entry_overlay`
(an allowlisted policy ID), and `dependencies` (`refuse-package`).

**Harness catalog** (`catalogs/<harness>.yaml`): `schema`, `harness`,
`evidence`, `accepts` (format IDs), `delivery.decompose.status`
(`verified` for all seven), `delivery.native` (`{status, format, probe}`
with `status` in `unverified`, `verified`, `unsupported`),
`surfaces.plugin.<kind>.<field>` (`keep`, `policy: <id>`, `omit-field`, or
`block-member` with a diagnostic `code`), and `root_token` (a
`hookpolicy.plugin_env` concept).

The check rejects unknown or duplicate keys, a `verified` native delivery
without a probe, with an undefined probe, or with a format the harness does
not `accept`, a field outcome outside the enum, a `block-member` without a
code, an unknown policy ID or format reference, and missing evidence refs.
Each failure names the file and field. The loader builds its indexes once.

### D3: Probe identifiers come from the harness validation suite

The harness validation suite (`tools/harness-compat`) owns the set of native
plugin probe identifiers. It defines none yet. The library validator takes
the known probe set as input; the suite's own test validates the shipped
catalogs against its set, so an undefined probe fails `make catalog-check`.

### D4: Plugin support reference

`tools/catalog-docs` also generates `site/docs/reference/plugin-support.md`
from the plugin catalogs: per harness target, accepted formats, decomposed
and native delivery status, and evidence date. `-check` fails when the
committed file differs. The nightly audit sources gain the plugin evidence
URLs.

### D5: Retire the Claude plugin manifest aggregate and the Factory claim

The Claude compiler stops emitting the `.claude-plugin/plugin.json`
fragment for skills and subagents, for every compile caller including
`convert`. The Claude native layout drops the two aggregates and the
`.claude-plugin` component, and the Factory layout drops `.factory-plugin`.
Earlier managed entries are retired through the existing marker-merge
pruning of uncovered managed entries, which already strips marked entries
and deletes files left with nothing. If that path does not reach the file
once no aggregate declares it, the Claude layout declares it as a retired
location that consumers reconcile marked entries out of and never write.
`sync --check` reports the pending removal as drift.

Plugin-form sources keep `.claude-plugin/plugin.json` as an ingestion
marker; only output changes.

### D6: Factory root `SKILL.md` warning

The Factory plugin form keeps loading skills only from `skills/<name>/`,
matching Factory's documentation. A root `SKILL.md` produces the warning
`ingest.factory.root-skill-ignored`, registered in the diagnostic catalog.

## Risks / Trade-offs

- [A released v4.1.4 client and this version share one checkout] → v4.1.4
  re-adds its entries and this version removes them again. Teammates use
  separate checkouts, so the released-client gate is unaffected.
- [Users' docs or scripts expect `.claude/.claude-plugin/`] → The how-to and
  reference pages drop it, and `.gitignore` advice no longer lists it.

## Migration Plan

No user action. The first `sync` after upgrading removes managed manifest
entries; handwritten entries stay.
