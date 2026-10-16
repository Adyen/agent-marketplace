# ADR 0010: Plugins are package resources, not loose bundles

**Status:** Accepted (2026-10-15)

## Decision

A plugin is a first-class resource in the harness-compilation library.
`ir.KindPlugin` is appended after `ir.KindRule` without renumbering, and
`ir.Plugin` carries the package identity: format ID and version, manifest
metadata, ordered members keyed `kind/name` holding typed leaf IR, a
contained file set with digests, inert extensions, and records of skipped
content. Each recognized package format (`claude-plugin`, `factory-plugin`,
`codex-plugin`, `agent-plugin`) is decoded by `plugindecode` under rules taken
from `target.PackageLayout` and the shipped `pluginpolicy` format catalogs;
Agent Plugins 1.0.0 is interpreted against bundled schema data and never
fetches `$schema`. `pluginpolicy.Project` — exposed as `CompilePackage` —
lowers members through the existing leaf compilers and applies the harness
catalog's `surfaces.plugin.<kind>.<field>` outcomes. The per-resource
`Compile` boundary refuses a plugin with `plugin.compile.package-required`.

## Context

Until now a plugin-shaped directory was only a source marker: ingesters
flattened it into leaf resources and `Compile` worked per leaf, so package
identity, the contained file set, and format failure boundaries had nowhere
to live. The plugin-policy-catalogs slice shipped the format and harness
catalogs but nothing decoded or projected through them.

## Consequences

Package failure boundaries (Agent Plugins §4–§9 and each native format's
rules) are enforceable: the package is rejected or a member is skipped at
the boundary the format defines, and a restriction the destination cannot
enforce blocks its member instead of installing without it. Output for
every existing resource stays byte-identical. Native catalog decoders,
configuration, package generations, and consumer wiring remain out of scope.

## Alternatives considered

| Alternative | Benefit | Limitation |
|---|---|---|
| Keep plugins as flattened bundles | No new IR or entry point | Loses package identity, the contained file set, and every format failure boundary |
| Decode inside each target ingester | Reuses existing discovery | Duplicates per-format rules per harness and bypasses the shipped catalogs |
| A neutral plugin manifest | One manifest shape for all formats | Contradicts each format's own schema and version rules; the charter now records plugin manifests as format-owned |
