## Context

This is the second slice of the v5.0.0 plugin epic (issue #6). The full plan
lives in the unarchived `plugin-packages` draft; this change lands its draft
tasks 1.4 and 2.1–2.4 (design D3 there). It is stacked on
`plugin-policy-catalogs`, which shipped the `pluginpolicy` format and
harness catalogs and `target/package_layout.go`, with every harness's native
delivery unverified.

Today a plugin-shaped directory is only a source marker: the ingesters
flatten it into leaf resources, and `Compile` works per leaf resource. The
library charter calls a plugin "just a bundle" and lists a neutral bundle
manifest as a non-goal.

## Goals / Non-Goals

**Goals:**

- One plugin IR that keeps package identity and member identity distinct.
- Decoders that follow each format's failure boundaries, including the
  Agent Plugins 1.0.0 rules.
- A pure projection that takes every plugin-surface decision from the
  shipped catalogs.
- Output for every existing resource stays byte-identical.

**Non-Goals:**

- Native plugin catalog decoders (marketplace formats).
- Configuration, selection, locks, package generations, native
  registration, export, CLI, and TUI.
- The plugin support reference.

## Decisions

### D1: Plugin IR

Append `KindPlugin` after `KindRule` without renumbering. A plugin carries
format ID and version, manifest metadata, ordered members keyed by
`kind/name` within the package holding typed leaf IR, a contained file set
(normalized relative paths, digests, executable bits), inert native
extensions, and records of unsupported or invalid content. Acquisition URLs,
commits, install roots, and ownership state are not plugin content. Plugin
names follow their format's grammar (Agent Plugins allows dots and a leading
digit), separate from the leaf `ir.ValidateName`.

The decoder builds the contained file set itself. Reusing the support-file
assembly for package generations belongs to the generation slice.

### D2: Package decoders

One decoder per package format reads the manifest path and component
directories from `target.PackageLayout` and the manifest field rules,
recognized versions, and discovery rules from the format catalog. Leaf
members decode through the existing loaders. Reads are bounded, duplicate
JSON keys are rejected, and paths that escape the package root, symlink
cycles, special files, and case-fold collisions are rejected with source
locations. Agent Plugins 1.0.0 is selected by its canonical `$schema`
identifier and validated against bundled schema data; any other identifier
is refused with `plugin.format.version-unsupported`.

### D3: Projection and package compilation

`pluginpolicy.Project(plugin, target, delivery, scope, packageRoot)` is
pure. It lowers members through the existing leaf compilers and the
model/tool/hook resolvers, applies the harness catalog's
`surfaces.plugin.<kind>.<field>` outcomes, and returns a package projection
plus value-free losses. A `block-member` outcome blocks the member with the
catalog's code and blocks the instance; `omit-field` and an unresolved
model drop only the field with a warning. `${PLUGIN_ROOT}` and
`${PLUGIN_DATA}` expand once, non-recursively, only in MCP `args`,
environment values, and `cwd`; `packageRoot` stands in for the package
generation until the generation slice supplies it. A public package compile
entry point wraps `Project`, and `Compile` given a plugin returns
`plugin.compile.package-required`.

The charter in `pkg/harness-compilation/AGENTS.md` is revised in the same
commit, with an ADR.

### D4: Conversion fixtures

`pluginpolicy/testdata/conversions.yaml` holds hand-written fixtures: a
package, a harness target, a delivery, and the expected projection and
diagnostics. The runner calls the public package compile entry point, runs
in `make catalog-check`, and names the fixture on mismatch.

### D5: Diagnostics

New code families `plugin.format.*`, `plugin.member.*`, and
`plugin.compile.*` join the diagnostic catalog. Diagnostics name plugin,
member, harness target, and stage, never secrets or command values.

## Risks / Trade-offs

- [The decoder's file set diverges from the later generation assembly] →
  The generation slice replaces or reuses it; the IR field shape stays.
- [Agent Plugins publishes 1.1.0] → It is refused until a decoder and
  catalog version are added deliberately.

## Migration Plan

No user-visible change; nothing to migrate. Rollback is a revert.
