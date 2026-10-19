## Context

`plugin-sources` locks plugins in `agent-marketplace-lock/v2` with
plugin-v1 per-layer member records, and `plugin-package-ir` provides
`pluginpolicy.Project`/`CompilePackage`, which lower a plugin's members for
one harness target given caller-supplied package and data roots. Sync still
filters plugin artifacts out in `pipeline.DeliverableArtifacts`. Closed PR
#273 (`origin/support-file`) assembled complete support bundles for the now
removed `convert` command.

## Goals / Non-Goals

**Goals:** decomposed delivery to all seven harness targets, package
generations as the single asset store, instance-level acceptance, removal
with plugin-data retention, and the #273 support-file assembly.

**Non-Goals:** native delivery and registration, delivery transitions and
per-request delivery choice, plugin CLI paths and TUI rows, export, and
documentation.

## Decisions

### D1: Package generations and persistent data

A package generation is an immutable, complete tree published atomically
under `$XDG_STATE_HOME/agent-marketplace/packages/<build-key>/`. The build
key covers package digest, `exclude`, harness target, delivery, scope, and
the plugin catalog fingerprint. Generations reuse the support-file assembly
(contained file set with digests and exec bits), are tracked in ownership
state, and stay as GC roots while any committed projection or recovery
journal references them. Plugin-root references resolve to the generation,
replacing the pointer into the acquisition cache.

Persistent plugin data lives at
`$XDG_STATE_HOME/agent-marketplace/plugin-data/<instance-key>/`, outside
generations and caches. It survives update and removal. Removal reports the
retained path.

### D2: Decomposed delivery

Members compile through the existing materializer as ordinary leaf
resources with installed name `<plugin>-<member>`, namespace version 1. A
collision with any other installed name in the same harness target and
scope, or a name that breaks the target's name rules, is refused with
`plugin.namespace-collision` or `plugin.namespace-invalid`. No suffixes are
added. Ownership records keep the plugin instance as the owner, so status
and removal act on the plugin. Delivered members are the post-exclusion
member keys committed per layer in the plugin-v1 lock manifests; same-layer
duplicate requests deliver the union of their exclusions.

### D3: Plugin instance acceptance rule

A plugin instance's members are planned together and committed through one
dependency transaction participant. If one member fails, the participant is
dropped and the prior projection's owned entries and generation reference
stay in place. Leaf resources keep their current per-resource behaviour.

### D4: Native requests stay pending

Until native delivery lands, a request with `delivery: native` keeps the
`plugin.delivery.pending` warning and is never decomposed, so later native
delivery cannot change an already decomposed installation by surprise.

### D5: Support-file assembly

Port PR #273's library changes without its `convert` command hunks. The
deleted command regressions become library-level `Convert` + `Materialize`
tests. Source trees are read through `os.OpenRoot(...).FS()`, with no
compile-time plugin root for author-machine sources.

## Risks / Trade-offs

- Generations duplicate package bytes per build key; `gc` bounds the cost.
- One participant per instance makes a large plugin's update all-or-nothing
  by design.
