## Context

`plugin-package-ir` added `ir.KindPlugin`, the package decoders
(`plugindecode.Decode`), `pluginpolicy.Project`, the format catalogs under
`pluginpolicy/formats/`, and `target/package_layout.go`. The consumer still
knows nothing about plugins: `internal/config/v4.go` accepts only `git`,
`directory`, `marketplace`, `ref`, `path`, and `source_harness` on sources
and `as`/`revision` on requests; collections accept skill, command,
subagent, hook, and MCP members; locks are `agent-marketplace-lock/v1`
(read) and `/v2` (write), strictly decoded, with frozen source-revision
contracts `v1` to `v4`.

This change is the sources, selection, catalogs, and locks slice of the
draft `plugin-packages` design (decisions D4 and D5, draft tasks 2.5,
3.1–3.4, 4.1–4.3). Delivery of plugin members, package generations, plugin
CLI inspection, and TUI rows belong to later slices.

## Goals / Non-Goals

**Goals:**

- Opt sources into package interpretation with `format`, and select plugins
  through the same configuration, resolver, and lock pipeline as other
  resources.
- Read native plugin catalogs into one outcome per entry, with source policy
  checked per origin.
- Lock plugin acquisition exactly inside the existing v2 lock format.
- Keep configurations, locks, source revisions, and output without plugins
  byte-identical.

**Non-Goals:**

- Harness delivery of plugin members, namespaces, and package generations.
- `list --kind plugin`, plugin inspection, `install`/`remove` plugin paths,
  status revisions, and TUI parent rows.
- A lock `apiVersion` change or any new lock field.
- Export, native registration, plugin dependencies, and hosted Marketplace
  plugin resources.

## Decisions

### D1: Configuration

```yaml
sources:
  team-plugins: {git: https://git.example.com/team/plugins.git, ref: main, format: claude-marketplace}
  portable:     {directory: ./tools, format: agent-plugin}
resources:
  team-plugins:
    - plugin/review-kit
    - plugin/lint-kit: {delivery: decompose, exclude: [command/legacy-review]}
  portable:
    - {kind: plugin, name: "1password.tools"}
```

- `format` is valid on `git` and `directory` sources only. Beside
  `marketplace` or `source_harness` it is `v4.option-combination`; an
  unrecognized value is `plugin.format.unknown`, naming the source and
  field. Recognized values are the format catalog IDs.
- A format source's catalog contains plugins only. A leaf locator into a
  format source that names a plugin member is refused with
  `plugin.member-direct-selection`, naming the plugin.
- `delivery` (`auto`, `decompose`, `native`) and `exclude` are valid only on
  plugin requests; `as` on a plugin is refused (`v4.option-combination`).
  `delivery` is parsed and carried; it has no effect until delivery lands.
- The structured `{kind, name}` entry carries names the short locator
  cannot. Editors write the short locator whenever the name allows it and
  otherwise keep the structured form. Identity comparison uses the authored
  name.
- Collections accept member kind `plugin`. A plugin selected by a
  collection stays one plugin.

*Alternative considered:* infer package interpretation from a plugin
manifest. Rejected: it would change existing loose ingestion of
plugin-shaped repositories.

### D2: Selection

Resolution records selection edges: direct request, collection-to-plugin,
and plugin-to-member. Members are keyed `kind/name` within their plugin, so
same-named members of two plugins stay distinct, and members are never
deduplicated against loose resources by content. `exclude` entries must
name exact members of that plugin (`plugin.member.unknown-exclusion`).

### D3: Native plugin catalogs

`plugindecode` gains decoders for `claude-marketplace`,
`factory-marketplace`, and `codex-marketplace`. Each returns one outcome per
entry: relative paths resolve against the catalog root from the package
layout (the directory containing the catalog manifest directory); Git and
Git subdirectory entries become exact acquisition requirements; source types
the format catalog marks `refuse` yield `plugin.catalog.source-unsupported`;
duplicate names mark every copy `plugin.catalog.duplicate-entry`. Claude
entry overlay semantics follow the format catalog's `entry_overlay` policy.
Every entry origin is checked against `allowed_registries` before it is
acquired.

### D4: Locks inside v2

No new lock version and no new lock fields. A plugin-selecting layer writes:

- Each format root as an ordinary `registries[].roots[]` entry whose
  `source_harness` is the format ID and whose `revision_schema` is a new
  source-revision contract, `plugin-v1`, used only for format roots. Its
  `revision_digest` commits to a manifest of the catalog index digest, each
  selected entry and its overlay digest, the format and version, and the
  selection edges (direct, collection-to-plugin, plugin-to-member minus
  exclusions).
- Each plugin as a `resources[]` entry with `kind: plugin`, the entry name,
  `content_digest` (package digest), `archive_digest`, and `support_files`
  (verified file inventory).
- Each external catalog entry as its own `registries[]` record with the
  entry repository's URL, ref, and exact commit, the entry subdirectory as
  the root path, and a `plugin-v1` manifest naming the owning catalog entry
  and its overlay digest.

Locked, offline, and check modes recompute the manifest from acquired
content and configuration and compare digests; a mismatch reports the
record stale and writes nothing. Roots without plugins keep `v4` and their
bytes. Released v4 clients refuse the configuration first; a `plugin-v1`
revision schema is also unknown to them, so they fail closed on the lock.

*Alternative considered:* a new `agent-marketplace-lock/v3` with plugin
records. Rejected: plugin records fit the existing record types, and a lock
version change for one resource kind is not justified.

### D5: Interim sync behaviour

Until plugin delivery lands, sync resolves, acquires, verifies, and locks
selected plugins, delivers every other resource normally, and skips harness
delivery of plugin members with a `plugin.delivery.pending` warning. No
harness file is written for plugin members. The delivery slice removes the
warning.

## Risks / Trade-offs

- [Selection edges are committed only through `revision_digest`] → Locked
  modes recompute them from configuration and locked content; a mismatch is
  reported stale rather than trusted.
- [`source_harness` carries a format ID on format roots] → The value space
  is disjoint from harness names, and only `plugin-v1` roots use it.
- [Interim warning ships on a stacked branch] → The delivery slice removes
  it before v5.0.0.

## Migration Plan

Opt-in through `format`; existing configurations and locks are unchanged.
Rollback: remove the plugin requests and `format`, then sync.
