# Target Profiles: Curated Packages for Non-Agent Apps

Status: brainstorm outcome, revised after adversarial review, inspection
of four existing Herdr app sources and comparison with the herdr-plugins
proposal on 2026-11-13; not yet approved for implementation. The normative
contract will live in OpenSpec changes (see Delivery plan); this document
records the agreed direction and the reasoning behind it.

The work is planned for CLI release v6. The consumer configuration format
stays `apiVersion: agent-marketplace/v4`, so no conversion is needed.
An executable, scratch-only Herdr app spike ran on 2026-11-13. It is not
approval to implement the design: the profile skeleton below remains
illustrative, and the spike neither freezes v1 nor implements generic target
profiles.

## Problem

Tools such as the Herdr app (mods, layouts), Herdr (plugins), tern
(plugins) and omp (plugins) install extensions directly from GitHub or
npm. Nobody vets that code, and there is no internal place to say "these
are the extensions we trust". Agent-marketplace already solves this for
agent resources: curated registries, Marketplace mirroring, analysis
assessments, revocation, exact locks, owned-file materialization and drift
checks. We want the same pipeline for other apps, starting with the Herdr
app, and we want app support to be addable without changing
agent-marketplace for every new tool.

Individuals are first-class users. Everything must work without an
organization layer; organization policy only adds restrictions or removes
prompts.

## Non-goals

- Preventing installs that bypass agent-marketplace (`herdr plugin install
  owner/repo`, copying files by hand). That needs host-side policy upstream
  or endpoint scanning; both are out of scope.
- A time-based quarantine window. Curation is manual: security vets a
  package and adds it to the curated registry, as with skills today.
- Translating host packages between apps. Translation stays a compiled
  agent-harness capability.
- Verifying a package's runtime dependencies beyond a PATH lookup.
- Limiting what a package does at run time. The Herdr app runs mods as
  trusted code with unrestricted commands and network access and has no
  per-mod permission setting, so neither the host nor agent-marketplace
  can hold a mod to a declared permission list. Packages carry no
  permission manifest (Decision 7).
- Repository-backed runtime installations for Herdr app packages. Keeping
  a whole repository snapshot does not make it an installed dependency.
- Running upstream install scripts, rewriting mod code or substituting
  machine-specific values into layouts. Authors supply valid native
  packages; users perform any declared manual configuration or activation.
- Changing the consumer `apiVersion`.
- Changes to the Herdr app or Herdr upstream. The design works with what
  they ship today.

## Spike findings (2026-11-13)

A throwaway Go spike tested whether one data-only `herdr-app` profile and
curator import declarations can normalize selected real sources, place them
with ownership and collision refusal, and run `herdr-app check`. It answers
only that feasibility question for one target. The primary source is the
[spike README](../../internal/prototype/targetprofile/README.md) on branch
`spike/target-profiles`, commit `4797afd88`; that link names a spike-branch
file, not a file promised on `main`. The local scenario log is disposable.

| Check | Result |
|---|---|
| Placement | 11 packages placed in a clean scratch home; 9 placed and 2 blocked as unowned collisions in a scratch home seeded with hand-installed pane-switcher and cos-queue. |
| Conflicts and ownership | Two new packages claiming the same files were both blocked in either selection order. An installed owner kept its projection when a newcomer claimed its files. |
| Import refusal and drift | Five negative probes (missing mod, symlink escape, destination outside the kind, shipped name, unsupported fragment) and a changed registry payload were refused. A modified deselected owned file was kept as drift while unchanged owned files were pruned. |
| Host check and runtime | Ten packages with mods passed initial-load checks; the theme had no host verification rule. `andrew-links` passed `herdr-app check` but Python `import helper` failed with `ModuleNotFoundError: file_link`; Chris's bundled `file_link.py` imported successfully. Queue placeholders also survived `herdr-app check`. |

The spike reused `fsboundary.SourceFS` for source reads without a fallback.
Its destination writes used a simplified scratch-only ownership and
filesystem path, not production ownership or materializer integration. It
did not test consent, authenticated assessments, root-bound state, a crash
journal, race protection, or destination symlink safety. Its sidecar hashes
were not authenticated, metadata identity was incomplete, and `requires`
matching was simplified. Profile roots, platforms and detect fields were
parsed but not enforced; hardcoded Herdr registry paths, kind checks and
layout-reference parsing leave second-target generality unproven. Per-file
renames were shown only on one filesystem, not package-atomic publication,
cross-filesystem behavior, or hot reload. The CLI accepts arbitrary paths,
so use it only with scratch homes. The host check loads code and may write
gpui-shell runtime data outside the scratch home; a scratch target is not a
sandbox or a no-side-effect guarantee. These are untested parts of the
design, not proven design flaws.

The prototype admitted the incomplete links and queue packages to expose
false confidence from initial-load checks. Neither is an acceptable
curated package as tested: curation must repair the missing packaged
dependency and unresolved queue values before admission. The checks do not
prove runtime readiness for all packages; the spike did not test real-window
activation or hot reload. It did not rewrite package code or run an upstream
installer.

## Second-target trial (2026-11-16)

The non-production runner `tools/target-profile-trial` places inert Herdr-
shaped and Tern-shaped fixtures with one declarative planner. Reference
pages retrieved 2026-11-16:

- [Tern packages and manifests](https://docs.stencil.so/tern/concepts/packages.html):
  packages contain `plugin.toml` and the entry files it names. The page
  documents macOS `~/Library/Application Support/Tern`, Linux
  `$XDG_CONFIG_HOME/tern` (fallback `~/.config/tern`), Windows
  `%APPDATA%\Tern`, and `TERN_CONFIG_DIR`; packages live under
  `<config>/plugins/`. It also says Tern stages changes in dot-named siblings.
- [Tern CLI reference](https://docs.stencil.so/tern/reference/cli.html):
  `plugin install` copies a package to `<plugins>/<id>`, using its manifest
  `id`, and stages it in a dot-named sibling before renaming it into place.
- [Tern distribution guide](https://docs.stencil.so/tern/guides/distribution.html):
  the source package directory contains `plugin.toml`; a repository with
  several plugins gives each one a directory. The install copy uses the
  manifest `id`, whatever the source directory is called. This page also
  describes dot-named staging directories.
- [Tern manifest reference](https://docs.stencil.so/tern/reference/manifest.html):
  `id` is unique among installed packages, and the package directory name
  need not match it.

Command: `CGO_ENABLED=0 go run ./tools/target-profile-trial`.
Observed result: exit 0; all cases passed:

| Case | Result |
|---|---|
| macOS defaults for Herdr and Tern | Herdr resolved under `~/.config/herdr-app`; Tern resolved under `~/Library/Application Support/Tern`. |
| Linux defaults without XDG | Herdr and Tern resolved under `~/.config/herdr-app` and `~/.config/tern`. |
| Linux defaults with XDG | Both roots used the injected XDG config directory. |
| Herdr override with spaces | `HERDR_APP_HOME` selected the scratch path; only `mods/night-light/{mod.json,main.js}` was placed. |
| Tern override with spaces | `TERN_CONFIG_DIR` selected the scratch path; `plugins/sample-tools/` directly contained `plugin.toml`, `host.luau` and `window.luau`. |
| Dot-prefixed Tern destination | The planner refused `plugins/.sample-tools/plugin.toml` and wrote nothing. |
| Source traversal | `../outside/mod.json` was refused with that path in the result. |
| Source symlink escape | `mods/night-light/mod.json` was refused as escaping the package source root. |
| Outside `into` allowlist | `themes/night.json` was refused against `[mods]`. |
| Outside package directory | `plugins/name-other-than-manifest-id/plugin.toml` was refused because the manifest identity expands the package directory to `plugins/sample-tools`. |
| Duplicate Tern destination, forward order | Both new package IDs were refused for each shared destination; the target root remained absent. |
| Duplicate Tern destination, reverse order | The same packages and destinations were refused; the target root remained absent. |

The generic root data used an override, per-OS `base` and `path`, and an
XDG fallback. The package data declared metadata and display exclusions.
Each kind supplied its `into` allowlist and directory template. Imports
mapped source files to root-relative native destinations. The planner checks
the allowlist first, then rejects dot-prefixed components and destinations
outside the expanded package directory. An optional identity rule names its
file, format, and field; the planner selects TOML or YAML parsing by format.
Negative-control runs that deleted a mapped fixture file or added an
unmapped one made the trial exit nonzero.
The exact-tree check showed that sidecar and display files stayed out.

This exercises only generic placement and simulated root selection. It
does not exercise host activation, runtime behavior, Tern's installer,
Windows roots, production race safety, crash recovery, ownership, or
approval. Tern's documented installer uses dot-named staging; this trial
applies files directly and rejects such entries. The trial sends absolute
overrides to paths inside its scratch directory. The design still needs to
resolve whether app overrides fit the engine's refusal of arbitrary
absolute roots (see Open items). The trial does not freeze profile v1.

## Decisions

### 1. Application target model and translation boundary

An **application target** is a destination the CLI may select and deliver
resources to, whether it is a compiled harness or a future host-package
target. Keep this authority in `internal/harness`. Evolve its existing
`InstallationAdapterRegistry` into the composition, lookup, list, and
accepted-kind seam for application target definitions. Compose compiled
targets from `target.Definitions()` and the current legacy `TargetInfo`
and native-layout adapter factories; do not add a third hand-maintained
map besides `TargetInfoMap` and the adapter registry. During migration,
adapt `TargetInfoMap` as a derived compatibility view. Devin, Codex, and
OpenCode remain adapter-backed and must never fall back to legacy metadata.

`pkg/harness-compilation` remains the authority for compiled translation
declarations and scope-neutral native topology. Do not move concrete root
resolution, profiles, selection, locks, ownership, distribution, or host
commands into it. Profile-only targets never enter `target.Definitions()`,
compiler or ingester registration, conversion targets, or translation
catalogs. The recommended declaration is
`target.Definition.AcceptedKinds []ir.ResourceKind` on the compiled
definition, derived once into `ApplicationTarget` metadata. Today's
compiled targets carry no such field; the observed matrix is that every
current compiled target accepts the five leaf kinds (`skill`, `command`,
`subagent`, `hook`, and `mcpserver`) and `plugin` packages, and only
Factory, Cursor, and Windsurf emit native `rule` artifacts. The
recommended `AcceptedKinds` lists those accepted kinds per target, with
`rule` declared only where the target emits rule artifacts. Derive the
selectable application view by intersecting the declared leaf kinds with
`config.AllKinds` excluding virtual `collection`, plus `ir.KindPlugin`
when the compiled `AcceptedKinds` declares it. `config.AllKinds` holds
only the five leaf kinds and `collection` (`internal/config/config.go`);
plugins ride the v4 ordered decision stream (`internal/config/v4.go`,
`v4KindToIR`) and the package path, so deriving acceptance from
`config.AllKinds` alone would silently lose the `plugin` kind. Keep plugin
package dispatch separate rather than adding a public selector or config field,
and do not add `plugin` to `config.AllKinds` or change existing selector
assumptions. Do not maintain an independent per-target application-kind
list. Rule's diagnostic-only translation paths remain available even
where `rule` is not accepted.

Define the internal seam explicitly: evolve `InstallationAdapterRegistry`
to compose `ApplicationTarget` entries. Each entry has an ID and
accepted-kind delivery metadata, and
`ApplicationTarget.Accepts(kind ir.ResourceKind) bool`; the registry
exposes `Lookup(id string) (ApplicationTarget, bool)` and
`Targets() []ApplicationTarget`. Existing installation adapters continue
to resolve scopes and destinations; they are not a new external schema.

The compiled-target `target.ValidateDefinition` paired-root check remains
a compiled-target constraint. A future user-only application descriptor
declares its scopes explicitly and does not run that validator. Repository
selection for a descriptor with no repository root must refuse before
resolving or joining an empty root. This bounded refactor changes neither
profile loading nor root behavior.

Use **delivery strategy** for the proposed `translate`, `place`, and
`register` choices. Keep the plugin library's existing **delivery mode**
(`native` or `decompose`) distinct, and keep **materialization mode**
(regular copies or cache-backed symlinks) independent of both.

For this design, **target** means an application destination; the existing
compiled **harness target** term retains its runtime-format meaning. A
**target profile** is future data that defines an application target, not
a compiler or translation registration. Use a qualifier for generic
packages (`host package`, `plugin package`): a plugin remains the
format-owned first-class package resource, and a host package is not
automatically an `ir.Plugin`. Do not edit the glossary or normative specs
in this design-only change.

### Delivery strategy and selection

| Strategy | Meaning | Examples |
|---|---|---|
| translate | canonical IR compiled to the native format | skills, commands, MCP servers for agent harnesses |
| place | package files copied into the target root, plus owned-entry merges into shared files | Herdr app mods and layouts, tern plugins |
| register | place files in an agent-marketplace store, then register them through the host's CLI | Herdr plugins, omp plugins |

A target may mix strategies per kind (omp is a translate target for skills
and a register target for plugins).

**Fan-out:** a resource is delivered to every enabled target that accepts
its kind. A skill reaches every enabled harness; `herdr-app/mod` reaches
only `herdr-app`. A host kind resolves only when its target is enabled
(Decision 10):

- A direct selection of a host package whose profile is not selected is a
  target-scoped configuration error, reported before acquisition; unrelated
  target work may proceed.
- A collection member whose target is not enabled is skipped and listed in
  `status`, without a diagnostic on every sync. This lets organization
  collections include app packages without writing into the homes of
  people who do not use that app.

A kind becomes portable only when two targets declare a translator for it.
Host kinds get no canonical IR until then.

### Current compiled-target kind matrix

Each current compiled application target accepts the same five
selectable leaf kinds and `plugin` package capability; `AcceptedKinds`
does not exist yet, so this matrix records observed behavior that the
recommended field would declare. `collection` remains
virtual selection expansion, not delivery. `rule` is not selectable at the
application boundary; the rule column records the existing library
behavior for diagnostics and output compatibility, not acceptance.
Kind acceptance does not promise that every value is representable:
hook events and restrictions, MCP fields, and other value-level limits
retain their current diagnostics.

| Application target | Skill | Command | Subagent | Hook | MCP server | Plugin | Rule (not selectable) |
|---|---|---|---|---|---|---|---|
| Factory | translate | translate | translate | translate | translate | package projection | Emits plain Markdown; `rule.factory.metadata-dropped` when metadata is lost |
| Claude | translate | translate | translate | translate | translate | package projection | No artifact; `rule.claude.no-primitive` |
| Cursor | translate | translate | translate | translate | translate | package projection | Emits native `.mdc` |
| Windsurf | translate | translate | translate | translate | translate | package projection | Emits native `.mdc` |
| Gemini | translate | translate | translate | translate | translate | package projection | No artifact; `rule.gemini.no-primitive` |
| Devin | translate | translate | translate | translate | translate | package projection | No artifact; `rule.devin.unsupported` (out of scope) |
| Codex | translate | translate | translate | translate | translate | package projection | No artifact; `rule.codex.unsupported` (out of scope) |
| OpenCode | translate | translate | translate | translate | translate | package projection | No artifact; `rule.opencode.unsupported` (out of scope) |

Factory, Cursor, and Windsurf are the only targets that emit native rule
artifacts. Claude and Gemini report no-primitive diagnostics.
Devin, Codex, and OpenCode report out-of-scope diagnostics; their codes
must not be rewritten as claims that the host lacks runtime ability.
Preserve these distinctions and their existing diagnostic codes.

### Plugin package policy

All eight compiled targets declare plugin package acceptance, but plugins
remain package projections rather than leaf `Compile` inputs or new public
selection fields.
`Compile(plugin)` continues to refuse with
`plugin.compile.package-required`; use `CompilePackage` and the existing
plugin catalogs. In the application registry, dispatch plugin handling
through the `translate` strategy's package path; never call leaf `Compile`.
The plugin library's `native` and `decompose` choices remain separate
existing plugin delivery modes. Every shipped plugin catalog has verified
decomposed delivery. A catalog's `accepts` list is native package-format
admission only, not a filter on decomposed member delivery.

| Target | Native `accepts` formats | Decompose | Native delivery |
|---|---|---|---|
| Claude | `claude-plugin`, `agent-plugin` | verified | user: verified `claude-plugin` via `claude-native-plugin`; repo: unverified |
| Factory | `factory-plugin`, `claude-plugin` | verified | unverified |
| Cursor | `agent-plugin` | verified | unverified |
| Windsurf | `claude-plugin`, `agent-plugin` | verified | unverified |
| Gemini | none | verified | unverified |
| Devin | `claude-plugin`, `agent-plugin` | verified | unverified |
| Codex | `codex-plugin`, `agent-plugin` | verified | unverified |
| OpenCode | none | verified | unverified |

These facts are recorded in `pluginpolicy/catalogs/*.yaml` and
`pluginpolicy.Project`; retain the `Compile` refusal and `CompilePackage`
path. Do not infer native plugin delivery from a format appearing in
`accepts` or from a package layout. Preserve plugin-member diagnostics
and whole-instance refusal behavior.

### Bounded `generalize-target-kinds` brief

Implement one application-target model and lookup/list/acceptance seam in
`internal/harness`, composing the compiled target declarations and the
existing legacy/native-layout installation factories. Resolve roots and
destinations through those adapters. Record the chosen
`target.Definition.AcceptedKinds []ir.ResourceKind` declaration on each
compiled target and derive it once into `ApplicationTarget` metadata;
do not restate a parallel per-target capability authority. Keep diagnostic-only rules separate
from successfully emitted kinds. Keep hook catalogs scoped to hook
translation, plugin catalogs to package projection, and model/tool
catalogs to the translation surfaces that use them. Profile-only targets
need none; retain all current catalogs.

Preserve existing output bytes and routing while composing the model:
Factory's forced command copies, Devin and Codex converted command skills,
Codex's partitioned destinations, Devin's scope-varying hook envelopes,
and OpenCode's native/owned aggregate paths remain unchanged.

The negative proof plan for the future implementation is:

- A synthetic partial target accepts a proper subset; selecting an
  unaccepted kind refuses before compile or write and names both kind
  and target, with no silent fallback to a skill.
- Missing implementation for an accepted kind fails completeness; a
  diagnostic-only rule produces no output and retains its existing
  diagnostic.
- A collection cannot reach `Compile`; a plugin still requires the
  package entry point; rejecting a plugin member preserves the whole
  previous instance.
- A user-only application descriptor never resolves a repository root;
  missing capability-specific registrations or catalogs fail only when
  that capability is required.

The checked-in test inventory distinguishes application acceptance from
compiler-library completeness:

- `internal/materialize/aggregate_inventory_test.go` contains
  `probeResources`, `TestAggregateProbeCoversEveryCompilableKind`, and
  `TestAggregateFilesMatchMaterializedMergeArtifacts`. Keep global probe
  coverage of every compilable leaf kind plus `rule`; `collection` is
  virtual, while `plugin` is tested through the package compiler. The
  per-target aggregate comparison uses accepted compilable leaf kinds,
  not diagnostic-only `rule`; a valid partial target with no shared
  aggregates may have an empty inventory.
- `pkg/harness-compilation/plugin_package_test.go`'s
  `TestCompileRefusesPlugin` and `TestCompilePackageProjects` preserve the
  leaf-compile refusal and package-dispatch path for plugins.
- Keep `internal/materialize/native_layout_test.go`'s existing
  `TestNativeLayoutMatchesCompiledArtifactPaths` and
  `TestDescribedTargetsDeclareNoRuleTopology` checks. The former proves
  scope-neutral native topology matches compiled artifact paths; the
  latter keeps declared accepted kinds distinct from topology and ensures
  descriptors do not imply rule topology. Future accepted-kind case
  selection can reuse these checks without inventing rule topology.
- `pkg/harness-compilation/golden_test.go`'s `TestGolden_AllTargets`
  continues to compare existing output trees byte-for-byte; never filter
  fixture bytes. `TestGolden_PerResourceKind` keys its five leaf cases to
  each compiled target's `AcceptedKinds`, keeps positive checks only for
  accepted kinds, and separately asserts a negative result for an
  unaccepted kind when exercising a partial compiled target. Neither test
  includes profile-only targets: `target.Definitions()` and
  `CompilerTargets()` remain compiled-only.
- Keep diagnostic-only rule behavior outside accepted-kind inventories:
  `pkg/harness-compilation/compile/factory/rule_test.go`
  (`TestFactoryRule_PlainMarkdown`, `TestFactoryRule_MetadataDropped`),
  `pkg/harness-compilation/compile/claude/rule_test.go`
  (`TestClaudeRule_NoPrimitiveDiagnostic`),
  `pkg/harness-compilation/compile/gemini/rule_test.go`
  (`TestGeminiRule_NoPrimitiveDiagnostic`),
  `pkg/harness-compilation/compile/cursor/rule_test.go`
  (`TestCursorRule_MDC`),
  `pkg/harness-compilation/compile/windsurf/rule_test.go`
  (`TestWindsurfRule_MDC`),
  `pkg/harness-compilation/compile/devin/translator_test.go`
  (`TestCompilerDropsRulesAsAnOutOfScopeWarning`),
  `pkg/harness-compilation/compile/codex/translator_test.go`
  (`TestUnsupportedRuleAndHookProduceStableDiagnostics`), and
  `pkg/harness-compilation/compile/opencode/translator_test.go`
  (`TestRulesEmitNoInstructionArtifact`) retain their current guarantees.
- `pkg/harness-compilation/target_registry_test.go` keeps
  `TestTargetRegistryIsComplete`, `TestEveryDeclaredTargetHasItsRegistrations`,
  and `TestEveryRegistrationIsDeclared` as compiler/ingester registration
  checks. `pkg/harness-compilation/target/target_test.go` keeps
  `TestDefinitionsHaveUniqueCompleteIdentity` and
  `TestValidateDefinitionRootModels` for compiled definitions and paired
  roots; app-only descriptors do not enter this registry or validator.
- Preserve native and package descriptor checks by name:
  `pkg/harness-compilation/target/native_layout_test.go`
  (`TestRegisteredNativeLayoutsValidate`,
  `TestHookAggregateTopologyIsDeclaredForEveryTarget`) and
  `pkg/harness-compilation/target/package_layout_test.go`
  (`TestValidatePackageLayouts`). These remain scope-neutral topology and
  package-format checks, not app-target registration.
- `internal/harness/registry_test.go`'s
  `TestInstallationAdapterRegistryCoverage`,
  `TestInstallationAdapterInventoryMatchesDescriptor`,
  `TestRegistryAllowsRepoResolutionWithoutHome`,
  `TestMigratedTargetsHaveNoLegacyMetadataFallback`, and
  `TestAdapterBackedMigratedRootRouting`, plus
  `internal/harness/roots_test.go`'s
  `TestTargetInfoMapCoversAllInstallRoots`, continue to prove adapter-backed
  routing, no Devin/Codex/OpenCode legacy fallback, and the `TargetInfoMap`
  compatibility view. Adapt registry coverage to the composed application
  set without weakening those invariants.
- `pkg/harness-compilation/hookpolicy/catalog_test.go` and
  `pkg/harness-compilation/toolpolicy/catalog_test.go` retain
  `TestShippedCatalogsValidate` and `TestEveryTargetShipsACatalog`;
  `pkg/harness-compilation/pluginpolicy/catalog_test.go` retains
  `TestShippedCatalogsValidate`, `TestEveryTargetShipsACatalog`, and
  `TestDecomposeDeliveryVerifiedEverywhere`, plus
  `TestNativeDeliveryVerifiedScopeRequiresProbe`,
  `TestVerifiedNativeDeliveryProbeRequired`, and
  `TestVerifiedNativeDeliveryForUnacceptedFormatFails`.
  `pkg/harness-compilation/modelpolicy/resolve_test.go` retains
  `TestShippedCatalogs`, and
  `tools/harness-compat/pluginprobes_test.go` retains
  `TestPluginCatalogsValidateAgainstNativeProbes`. If a partial compiled
  target is tested, only catalog checks relevant to its declared accepted
  capabilities become capability-specific. Keep every current
  compiled-target catalog and native proof requirement. `target.Targets()`
  remains compiled-only, so catalog checks do not gain app-only targets
  or weaken the current compiled-target guarantees.

Existing regression commands, checked against the test names above and
the rule compiler tests:

```sh
go test ./internal/materialize -run '^(TestAggregateProbeCoversEveryCompilableKind|TestAggregateFilesMatchMaterializedMergeArtifacts)$' -count=1
go test ./pkg/harness-compilation -run '^(TestTargetRegistryIsComplete|TestEveryDeclaredTargetHasItsRegistrations|TestEveryRegistrationIsDeclared|TestGolden_AllTargets|TestGolden_PerResourceKind|TestCompileRefusesPlugin|TestCompilePackageProjects)$' -count=1
go test ./pkg/harness-compilation/target -run '^(TestDefinitionsHaveUniqueCompleteIdentity|TestValidateDefinitionRootModels|TestRegisteredNativeLayoutsValidate|TestValidatePackageLayouts)$' -count=1
go test ./internal/harness -run '^(TestTargetInfoMapCoversAllInstallRoots|TestInstallationAdapterRegistryCoverage|TestInstallationAdapterInventoryMatchesDescriptor|TestRegistryAllowsRepoResolutionWithoutHome|TestMigratedTargetsHaveNoLegacyMetadataFallback|TestAdapterBackedMigratedRootRouting)$' -count=1
go test ./pkg/harness-compilation/pluginpolicy -run '^(TestEveryTargetShipsACatalog|TestDecomposeDeliveryVerifiedEverywhere|TestProjectDecomposeIgnoresNativeAccepts)$' -count=1
go test ./pkg/harness-compilation/hookpolicy -run '^(TestShippedCatalogsValidate|TestEveryTargetShipsACatalog)$' -count=1
go test ./pkg/harness-compilation/toolpolicy -run '^(TestShippedCatalogsValidate|TestEveryTargetShipsACatalog)$' -count=1
go test ./pkg/harness-compilation/modelpolicy -run '^TestShippedCatalogs$' -count=1
go test ./pkg/harness-compilation/compile/... -run 'Rule' -count=1
```

The existing catalog validator and generator entry points are
`make catalog-check`, `make model-catalog-check`,
`make model-catalog-audit`, and `make catalog-docs`; the latter runs
`tools/catalog-docs`, while `catalog-check` also runs
`tools/harness-compat`'s `TestPluginCatalogsValidateAgainstNativeProbes`
and `tools/catalog-docs -check`.

These are future implementation regressions, not runtime trials authorized
by this design-only assignment.

### 2. `targets:` in consumer configuration

```yaml
apiVersion: agent-marketplace/v4
targets: [claude, devin, herdr-app]
```

`targets:` is the same list as `harnesses:`. `harnesses:` stays as an
alias and is deprecated later. A file declaring both is refused.
Resource-group narrowing (`targets:` inside a group, formerly
`harnesses:`) gets the same alias and keeps its "narrows, never widens"
rule.

**Rollout.** v4 parsing rejects unknown fields, so a released v5.x client
refuses a file that contains `targets:`. In a committed repository
configuration that would break teammates on an older CLI. The switch
therefore follows the read-before-write rule:

1. v6.0 reads both spellings. Editors keep the spelling a file already
   uses and write `harnesses:` into a file that has neither.
2. A later release makes editors write `targets:` by default.
3. A release after that deprecates `harnesses:`.

CLI releases older than v5 are not a compatibility constraint for this
change.

**Target IDs stay statically valid.** Every ID in the list must be a
compiled target or the ID of a `target-profile/<id>` selected in the user
or organization layer. Anything else is a configuration error before
acquisition, so the rule that the CLI understands every configured target
(ADR 0010) keeps holding and a typo is still detected.

A target that declares no repository root (Herdr app, tern) accepts
selections only from user and organization layers; selecting its kinds at
repository scope is refused with a diagnostic.

### 3. Target profiles: targets as data

A **target profile** is future data defining an application target that
uses `place` and/or `register` delivery strategies, without Go code.
Translate delivery stays compiled. Built-in harnesses remain compiled
targets. The Herdr app is the first profile; it ships from a built-in
source and goes through the same selection, lock and approval path as a
profile from any other source, so the format and the external path are
exercised by real use from the start.

**One profile per target.** All Herdr app mods and layouts use the same
`herdr-app` profile. Its revisions define the host contract: accepted
kinds, permitted destinations, aggregate handling and host commands.
Different repository layouts do not create profile variants. Package
discovery and source-to-destination mappings belong to package metadata
or curator-supplied import declarations (Decision 4), and cannot widen
the profile's permissions.

Profile skeleton (field names illustrative, fixed in the OpenSpec change):

```yaml
apiVersion: agent-marketplace.io/target-profile/v1
id: herdr-app
label: Herdr app
platforms: [macos, linux, windows]
detect: { any: [{ binary: herdr-app }, { root: user }] }
roots:
  user:
    env: HERDR_APP_HOME
    default: { macos: ~/.config/herdr-app, linux: ~/.config/herdr-app }
host:
  binary: herdr-app
  version:  { probe: [herdr-app, --version], pattern: '(\d+\.\d+\.\d+)' }
  verified: [{ range: ">=X.Y.0 <X.Z.0", date: 2026-11-12, evidence: <ref> }]
  shipped:  { mods: [tabs, agents, files], layouts: [main, terminals, win98] }
package:
  metadata: agent-marketplace.yaml                      # curation sidecar
  display:  { readme: RECIPE.md, preview: preview.png }
kinds:
  mod:
    deliver: place
    place:   { into: [mods/] }
    verify:  [herdr-app, check, "{path}"]
  layout:
    deliver: place
    place:
      into: [layouts/, mods/, backgrounds/, themes/]
      files: [gitlab.json, review-prompt.md, rereview-prompt.md]
  theme:
    deliver: place
    place:   { into: [themes/] }
  icons:                                  # with aggregates (Decision 5)
    deliver: place
    place:   { into: [program-icons/] }
```

Place delivery copies the normalized package payload to the same relative
paths under the target root, except aggregate fragments (Decision 5).
Curation metadata and declared display files are not payload. `into`
lists permitted top-level directories; `files` permits exact root-level
support files. A payload outside those declarations is refused as a whole,
at Marketplace ingestion and at sync, instead of being dropped silently.
Support-file declarations do not authorize replacing unowned files or
writing shared aggregates as whole files. `verify` is optional (Decision 6).

`theme` and `icons` are kinds of their own, so a color theme or a set of
program icons can be shared without a layout. A layout may still bundle a
theme that only it uses. `icons` places its files and adds entries to
`programs.json`, so it ships with the aggregates change (Decision 5).

`host.shipped` lists the mods and layouts the host itself installs for
the verified range. The Herdr app records them in `installed.json` in its
root and replaces them on every start. Ingestion and the author dry-run
refuse a package that claims one of these names (Decision 4).

Herdr app delivery is a **self-contained native installation**. Its
package-owned code and assets must work from their installed paths,
without reading an upstream checkout or relying on repository symlinks.
A layout may bundle mods and declared support files that only it uses,
and requires the packages it shares with other layouts (Decision 4).
Runtime data, host-provided facilities and programs found on PATH remain
separate from those packaged dependencies.

Register-mode example, using the real Herdr CLI (`plugin link <PATH>`,
`plugin unlink <ID>`, `plugin list --json` returning `result.plugins[]`
with `plugin_id` and `plugin_root`):

```yaml
id: herdr
host:
  binary: herdr
  platforms: [macos, linux]
kinds:
  plugin:
    identity: { file: herdr-plugin.toml, format: toml, field: id }
    deliver:  register
    register:   [herdr, plugin, link, "{path}"]
    unregister: [herdr, plugin, unlink, "{id}"]
    inventory:
      command: [herdr, plugin, list, --json]
      items: result.plugins
      id: plugin_id
      path: plugin_root
```

The format must also fit tern without special cases: tern places
`<config>/plugins/<id>/` with per-OS roots (macOS `~/Library/Application
Support/Tern`, Linux `$XDG_CONFIG_HOME/tern`, Windows `%APPDATA%\Tern`) and
a `TERN_CONFIG_DIR` override. Tern skips dot-named plugin entries and stages
changes in dot-named siblings. Keep the paper test against real Herdr app
layouts, tern and omp, and retain further trials before freezing v1. The
scratch-only Tern trial above exercises declarative root overrides, paths
with spaces, package topology and metadata exclusions without a target-ID
switch; Tern remains a candidate, not an approved v1 profile. Herdr layout
dependency checks are host-specific author/analysis validation, not a
generic place-engine feature. The trial must not introduce arbitrary
code-executing validation to make placement work.

#### Engine-owned guarantees (profiles cannot relax them)

- Nothing in a profile takes effect before that profile revision is
  approved (Decision 7).
- Never overwrite unowned files. This also protects an app's shipped
  files (for example Herdr's built-in mods), so sync needs no reserved
  name list. `host.shipped` only serves the earlier check at curation.
- Writes stay inside the declared root, with existing `fsboundary`
  symlink containment. Roots resolve under `$HOME` or the OS/XDG config
  and data directories; arbitrary absolute roots are refused. A root may
  not be `$HOME` or an XDG base directory itself, and may not overlap
  agent-marketplace's own directories. This needs reconciliation with
  app-specific absolute root overrides before profile v1 is frozen.
- A profile cannot claim another target's id or overlap its roots.
- No dot-prefixed staging siblings inside target roots.
- Lock, drift, `sync --check`, backup and pruning behave as for every
  other target.

### 4. Herdr app packages in the curated registry

#### Source import and package boundaries

Git origin, ref and optional subdirectory identify the source. An author
or curator declares package boundaries and explicit file/tree mappings
from that source to native destination-relative paths. The declaration
lives in the registry-side curation sidecar (Decision 13); upstream
repositories need not adopt the curated registry layout.
The metadata syntax is fixed in the OpenSpec change, not by adding
per-repository target profiles.

This supports a directory of native mods, a single mod, or a complete
personal app setup. For example, an import can map `mods/chris-links/`
to the same native path, while a source rooted at `mods/` maps
`andrew-tabs/` to `mods/andrew-tabs/`. A complete setup declares its
layouts, bundled mods and permitted root-level support files as one
package. Root files such as `gitlab.json` and review prompts receive
the same ownership and collision protection as other placed files.
`keys.json` remains an aggregate fragment, with merging deferred to
Decision 5.

All mapped inputs must stay within the selected source root, including
through symlinks. The installed package must also contain its declared
package-owned dependencies at usable native paths. Selecting a larger
source root can make a dependency available for packaging, but does not
make a runtime reference to an uninstalled repository sibling valid.
The importer neither follows such references outside its source root
nor rewrites code to repair them.

Import declarations select payload, display content and aggregate
fragments explicitly. Repository development files and installers need
not become payload, but every selected payload file must have an allowed
destination. Imports preserve payload bytes; authors or curators make
any necessary code changes before review. Sync never executes
`install.sh`, `make link` or equivalent upstream installation logic.

#### Curated representation

After import, vetted package bytes are stored verbatim under `payload/`,
next to a curation sidecar. This normalized registry representation uses
the same `packages/<target-id>/<local-kind>/<name>/` layout as Decision 13;
it is not a required upstream repository layout:

```
packages/
  herdr-app/
    mod/<name>/
      agent-marketplace.yaml  # curation sidecar, written by the curator
      payload/mods/<name>/main.js, mod.json, ...
      payload/keys.json       # optional mapped fragment (Decision 5)
      RECIPE.md, preview.png
    layout/<name>/
      agent-marketplace.yaml
      payload/layouts/<name>.json, <name>.svg
      payload/mods/<dep>/...  # bundled mods only this layout uses
      payload/backgrounds/..., themes/...
      payload/gitlab.json     # optional, explicitly permitted support file
      payload/review-prompt.md, rereview-prompt.md
      RECIPE.md, preview.png
    theme/<name>/
      agent-marketplace.yaml
      payload/themes/<name>.json
      RECIPE.md, preview.png
    icons/<name>/             # with aggregates (Decision 5)
      agent-marketplace.yaml
      payload/program-icons/<name>/*.svg
      payload/programs.json   # fragment
      RECIPE.md
```

What security reviewed is byte-for-byte what is stored and served. Upstream
folders carry no agent-marketplace metadata, so the curator adds
the **sidecar**: name, description, tags, `metadata.requires` (the
packages it needs, below), the programs it runs and the host versions it
was tried on (Decision 8), platforms, upstream URL and commit,
`cloned_from` when it started as a copy of a shipped mod (Decision 9),
import mapping/provenance, manual setup requirements and any curator-
supplied digest claims. Those claims are unauthenticated input, not an
assessment; Decision 13 defines the computed identity. Display files such
as `README.md`,
`RECIPE.md` or `preview.png` are declared explicitly. The sidecar and
declared display files are shown by the Marketplace UI and never placed.
The assessed package identity covers the delivery mapping as well as
the payload, so changing a destination requires a new assessment.

#### Dependencies between packages

A package declares the other packages it needs with `metadata.requires`
in its sidecar. This is the existing resource-prerequisites contract: a
list of `kind:name` strings or maps with `name`, `kind` and optional
`registry` and `path` hints, met by any selected resource with that kind
and name. A layout that uses someone else's mod requires it:

```yaml
metadata:
  requires:
    - herdr-app/mod:andrew-tabs
```

Reuse needs three extensions, fixed in the OpenSpec change:

- Host kinds are accepted as prerequisite kinds and printed in warnings
  and remediation. Today an unknown kind is dropped with
  `catalog.requires.invalid`, which is also what a released client does
  with a host kind.
- The sidecar becomes a declaration site. Today only the front matter of
  skills, commands and subagents is read.
- A requirement on a host package whose target is not enabled follows the
  collection-member rule in Decision 1.

Selection follows the prerequisite stack that is open as pull requests
#626, #643, #661, #671 and #672 (state on 2026-11-13) and inherits its
rollout:

- Without `requires.expand`, a requirement is advisory. Selecting a layout
  warns about each unselected mod and prints its install command.
- With `requires.expand: true`, the resolver selects required packages
  transitively from configured Git and directory catalogs (#626) and
  reports them in `install`, `list` and `status` (#643).
- For Marketplace sources the stack only reads requirements (#661). A
  match is previewed and reported as `unrecorded`; recording it in the
  lock waits for the release that makes expansion the default. Until
  then a Marketplace user selects the required mods explicitly, or the
  curator publishes a collection holding the layout and its mods.
- The "Requires" and "Required by" sections in the TUI (#671) and on the
  Marketplace resource page (#672) show host packages without
  target-specific code.

A layout placed without a required mod does not work. Package views
report an unmet requirement next to pending manual setup, never as ready
(Decision 11). Whether such a layout is placed at all is an open item.

`requires` has no version constraint. Keeping a layout and the mods it
requires compatible within one registry revision is the curator's job.

Programs a package runs (`gh`, `python3`) are not `requires` items; see
Decision 8.

#### Destination collisions

A destination path has one owning package. A mod that several layouts
use is its own package, and those layouts require it instead of each
bundling a copy. A package still bundles the mods and support files that
only it uses.

- Two packages that claim the same destination path conflict, even when
  their files are byte-identical.
- A package that claims a mod or layout name in the profile's
  `host.shipped` list is refused at ingestion and in the author dry-run.
- Conflicts are resolved across the complete desired package set before
  publication, independent of package iteration order, with a diagnostic
  naming the conflicting packages and paths. An installed package keeps
  its complete installed projection. A new package, or a new revision of
  an installed one, that claims a path owned by another package installs
  nothing. Two new packages that conflict with each other both install
  nothing. Independent packages still proceed.

Placing a layout and the mods it requires is several filesystem writes,
and a running host can observe the state in between; the proposed
publication and recovery contract is Decision 14.

Upstream resolves a name clash by renaming the incoming mod. Curators do
the same at curation time, or split a shared mod into its own package;
this is why packages are stored "as reviewed" and not "as upstream".
Importing a complete personal setup whose mods already exist as packages
needs the same curator choice: require the existing package, or rename.

Sharing byte-identical bundled trees between packages was considered and
deferred. It needs multi-owner pruning, journaled shared ownership and
conflict handling across connected groups of packages, and it only helps
when sources nobody curates bundle the same mod.

#### Existing-source acceptance examples

These sources were inspected on 2026-11-13. They define the shapes the
import design must handle, not a claim that their current contents are
already curated or installable:

| Source | Finding | Intended treatment |
|---|---|---|
| ai-configs: resources/herdr-app | `pane-switcher` contains native `mods/pane-switcher/` payload plus a README and installer. The installer also binds keys and opens a kept window in layouts. | Import the native payload and declared documentation. Report bindings and kept-window activation as manual setup in v1; do not run the installer or silently claim the setup is complete. |
| ai-configs: enqueue/herdr | The queue bundle has native mod/layout paths, but its layout contains `@@PYTHON@@`, `@@SCRIPT@@` and `@@COS_HOME@@`; `cos_queue.py` is outside the selected subtree. | The author must fix packaging and configuration. Placement alone cannot make this a working bundle; the engine does not substitute paths, acquire the missing script or add register behavior to repair it. |
| chrisj/herdr | A complete setup contains mods, a layout, keys, `gitlab.json` and review prompts. The layout contains blocks needed by the pollers. | Positive model for a complete native package: import the layout, mods and explicitly permitted support files together. Treat keys as aggregate advice until merging ships, and report the manual layout activation. |
| andrewz/herdr-plugins: mods | Native mod folders can be mapped, but `andrew-links/helper.py` imports `plugins/file-link/file_link.py` outside the selected root and depends on checkout topology. | Refuse that incomplete package; independent self-contained mods remain eligible. The author must bundle the dependency at a supported native path and fix the reference. Archiving the entire repository alone does not repair installation. |

These are place-delivery packages, followed by optional verification.
A missing runtime dependency or unresolved setting can survive a host
syntax/load check and fail only during use. The UI distinguishes files
placed, checks passed and manual setup still required. It does not infer
functional readiness from successful placement.

The spike confirmed both false positives: `andrew-links` passed the host
initial-load check although its installed Python helper could not import
`file_link`, and the queue placeholders survived the same check. It
deliberately admitted these incomplete sources to expose the gap; they are
not acceptable curated packages until curation repairs the missing
packaged dependency and queue values. Manual setup for activation,
keybindings or machine configuration does not replace missing
package-owned code. Static checks must not claim to find every dynamic
reference or every placeholder syntax.

### 5. Owned-entry merges into shared app files (second Herdr app change)

Not part of the first Herdr app delivery; mods, layouts and themes come
first. This is the next change after it and ships before register
delivery: several inspected mods are not usable without their bindings,
and the Herdr app needs no register mode. Until it ships, sync places
nothing in `keys.json` and reports a package's suggested bindings as
advice.

Profiles declare a shared file the way compiled targets declare MCP
configuration. The existing ownership record is a container plus a key
(`mcpServers` plus the server name), which maps directly onto a
`keys.json` section plus key:

```yaml
aggregates:
  keys:
    file: keys.json          # relative to the target root
    format: json             # an existing format-preserving patcher
    fragment: keys.json      # package file that supplies entries
    containers: ["global", "terminal", "layout:*", "mod:*"]
    on_edit: cede            # or restore
  programs:
    file: programs.json
    format: json
    fragment: programs.json
    containers: ["programs"]
    on_edit: restore
```

The merge reuses the existing owned-entry rules: ownership is proven from
durable state, an unmanaged entry with the same identity is preserved and
only the generated entry is refused, and deselection prunes only owned
entries. The engine needs two additions:

- A generic entry kind. Today's kinds are MCP server and hook.
- A container allowlist declared by the profile, because containers come
  from the package here instead of a fixed vocabulary.

`on_edit` chooses what happens to an owned entry the user changed:

- `restore` is today's MCP rule: sync puts the configured value back.
- `cede` hands the entry to the user. It suits key bindings, where
  `restore` would overwrite a rebinding on the next sync. A reset or
  deleted file (the Herdr app escape hatch moves `keys.json` aside) cedes
  its owned entries too. The exact drift and re-add rules are fixed in the
  OpenSpec change, which amends `owned-entry-reconciliation`.

`programs.json` is the second Herdr app aggregate. Its `programs` map is
keyed by program name, and the `icons` kind adds entries there that point
at the icon files it placed. Its `shells` and `interpreters` lists are
not keyed containers and stay unmanaged.

A package may consist of fragments only, such as a shortcut pack without
a mod. Whether that is a kind of its own is fixed in the OpenSpec change.

The Herdr app treats any edit to `keys.json` as making the whole file the
user's, and then stops adding new default shortcuts on upgrade. Without an
upstream change the first merge can only warn about this, once.

### 6. Host commands: register lifecycle and verification

Register lifecycle:

1. **Stage** into an agent-marketplace store, one directory per revision:
   `<data>/targets/<target>/<id>/<revision>/`. Never into the host's own
   directory. Upgrades add a directory and re-register, so running host
   processes keep their files.
2. **Inventory and classify ownership** before changing a registration.
   An absent ID permits installation. An existing ID permits an upgrade
   only when durable ownership proves that it points to the exact
   previously recorded generation. Every other registration blocks the
   operation and remains untouched, even if its path is under our store.
   Being under the store is not ownership proof.
3. **Journal** the previous and desired registration paths, generation
   references and the approved profile revision that supplies the
   commands before running the transition.
4. **Register** with the declared argv. Failure leaves the staged
   generation and journal pending and reports it. A later mutating sync
   inventories the host before deciding whether to retry.
5. **Confirm and commit ownership** only after live inventory identifies
   the exact desired generation path. ID presence alone does not prove
   an upgrade succeeded. Owned and missing is drift; an unexpected path
   is a conflict.
6. **Unregister** deselected owned registrations only after live inventory
   confirms the exact recorded path and the removal is journaled.
   Confirm absence before committing removal and allowing generation
   pruning; preserve any generation still referenced elsewhere.

Recovery distinguishes the recorded previous state, the desired state
and an unexpected state. The previous state permits retry; the desired
state permits completing the journal without repeating the command. An
unexpected registration stops the transition for repair, without
unlinking it or overwriting it through a retry. Missing registrations
may be repaired only while the package remains selected and its
execution authority is valid.

Each register profile documents the host command's collision behavior.
Agent-marketplace serializes its own transitions for a target, but
inventory before a command cannot prevent another process from changing
the registration between those steps. A host that lacks a conditional
update API cannot provide that stronger guarantee; post-command
inventory detects observable divergence and preserves pending evidence
instead of attempting a destructive rollback.

**Verification.** A kind may declare `verify`, a command that checks one
delivered package, such as `herdr-app check <mod folder>`. It runs after
place or register in a mutating sync. A failure is reported, makes sync
exit nonzero and is recorded. Decision 14 chooses to retain the placed
package after a failed check and defines its recovery and retry state.
Herdr app mods use place followed by verify, not register. Verification
reports only what the host check exercised; it does not prove all runtime
paths or user configuration are valid, and it does not repair them.

Keep distinct results for files placed, the named check and its scope,
exact package/profile revisions and host version, pending manual setup or
unmet requirements, and runtime acceptance evidence only when actually
exercised. The host documents `herdr-app check` as an initial-load and eager
element check, not a check of deferred renders or later runtime paths.
The spike demonstrated the gap: `andrew-links` passed the check but its
Python helper import failed, while queue placeholders also passed.
Never infer functional readiness from a
successful host check, no warnings, or an empty `manual_setup`. Host-version
evidence and runtime witnesses are curator evidence, not a new
consumer-configured command surface.

When host commands run:

| Command | Runs |
|---|---|
| mutating `sync` | version probe, register, unregister, inventory, verify |
| `sync --check`, `status`, TUI drift views | nothing; they report the recorded inventory and verify results |
| `sync --check --verify`, `doctor` | version probe, inventory and verify, live; never register or unregister |
| author import dry-run | nothing; discovery, validation and placement planning only |
| explicit author live verification | approved version probe and verify against the selected installed package; never register or unregister |

The default read-only path executes nothing for two reasons. Those
commands run unattended in CI, pre-commit and session-start hooks. And a
verify command can load package code: `herdr-app check` loads the mod. The
cost is that an out-of-band `herdr plugin unlink` shows up only at the
next mutating sync or live check. The profile declares the command; the
user decides when it runs outside a mutating sync.

Safety rules:

- No host command runs before the profile revision is approved
  (Decision 7).
- Every argv must start with `host.binary`. No shell, no other programs.
- The binary is resolved to an absolute path at approval and recorded
  with it. Later runs execute the recorded path, not a fresh PATH lookup.
  A path inside the current repository, or reached through a relative PATH
  entry, is refused. A changed path asks for approval again.
- Templates substitute only `{path}`, `{id}`, `{version}`, each as exactly
  one argv element.
- Stdin closed, per-command timeout, bounded and redacted output.
- Journal recovery uses the exact previous and desired registration
  states above; it never treats ID presence alone as success.
- A missing host binary makes the target unavailable for that run; other
  targets proceed.

### 7. Consent: profile approval and package assessment

Every profile revision needs approval before it takes effect, whatever
its delivery mode. Today the binary decides where a source's content
lands, and Marketplace analysis judges a resource knowing that
destination. A profile moves that choice to the source, so reviewing a
package only means something together with the profile revision that
places it.

**Assessment binds the package to its profile.** The Marketplace records
the exact profile revision digest used to interpret and assess each host
package, as part of authenticated assessment identity. Before delivery,
the client verifies both the exact package identity and that its selected
profile matches this digest. The same check applies to online resolution
and offline delivery from cache; missing or mismatched bindings refuse
the affected delivery.

Profile approval authorizes destinations and commands. Package assessment
authorizes the package under that profile; neither substitutes for the
other. A profile update needs a matching assessment before a Marketplace
package can use it. V1 binds the full profile digest, accepting that even
an evidence-date change requires reassessment. A narrower digest of
delivery behavior is deferred until that cost justifies a compatibility
contract.

Git and local-directory packages retain their existing source trust
rules. They cannot inherit a Marketplace assessment made under a
different profile. Marketplace ingestion must select the assessment
profile explicitly and preserve its digest through publication and
client verification.

**Assessment is review, not a sandbox.** The Herdr app's guide (checked on
0.1.15) says mods are trusted code whose commands and network access,
including `fetch`, are unrestricted. `mod.json` has no permission field
and the app has no per-mod setting. A mod can start any program through
`exec`, so a list of what it calls, derived from its source, is
incomplete by construction: arguments can be computed, `sh -c` runs
anything, and a helper script in another language is outside the
analysis. Packages therefore declare no permissions, and nothing in this
design claims to limit a package at run time.

The pipeline provides three things instead:

- The bytes that were reviewed are the bytes that are delivered.
- Analysis produces a capability summary: whether the package runs
  programs, reaches the network or reads files outside its own folder.
  These are the three things the Herdr guide tells an installer to
  report before adding a mod. The summary is shown to reviewers and
  users as disclosure and is labeled as analysis output.
- A package that turns out to be harmful is revoked.

**Local approval binds resolved destinations.** Approval is recorded in
user state against the profile revision digest, canonical resolved
destination roots and resolved host executable path, when applicable.
The first sync of a target shows what the profile does: its roots, place
rules, aggregate files, the exact command set and the resolved host
binary. A new profile revision or a change to either resolved roots or
executable path shows a diff and requires approval again.

Changing `HERDR_APP_HOME`, XDG settings or a root alias can change the
destination without changing profile bytes. Until the new destination is
approved, the target reports "target destination changed" and writes
nothing at either location. Ownership stays bound to the recorded
installation roots. Approval of the new location allows a new
installation there; it does not authorize cleanup of the old one.
V1 does not automatically relocate installations: status shows both
locations, and retiring the old installation requires an explicit action
using its recorded ownership and execution authority.

Headless sync skips an unapproved target with a remediable diagnostic;
other targets proceed, so one pending approval never blocks agent
resources. An organization layer may pre-approve specific profile digests
with explicit destination and executable-binding rules for hands-off
rollout. A digest alone does not authorize arbitrary environment-derived
destinations. The client records the concrete approved bindings on each
machine; a binding outside those rules requires interactive approval.
Without organization preapproval, individuals approve interactively.
Enabling a target is not by itself consent.

### 8. Conditions on the host machine

| Condition | Declared by | Check | Effect |
|---|---|---|---|
| Platform | profile, package sidecar | runtime OS | hard: skip elsewhere |
| Host present | profile `detect` | PATH lookup (no execution) or root exists | hard: target unavailable this run |
| Host binary (register) | profile `host.binary` | PATH lookup, absolute path recorded | hard |
| Host version | profile `host.version` | single probe with fixed argv, run only when host commands may run (Decision 6) | soft: warn outside `verified`; organization policy may block |
| Package host version | package sidecar `host: { min, tested }` | compared with the last recorded version probe | soft: warn below `min` or above `tested` |
| Required packages | package sidecar `metadata.requires` | a selected resource with that kind and name (Decision 4) | advisory; selected automatically with `requires.expand` |
| Programs the package runs | package sidecar `binaries: [gh, python3]` | PATH lookup only | advisory warning |

Programs a package runs have their own sidecar field. They are not a
`binary` kind inside `metadata.requires`: a `requires` item names a
resource that can be selected, locked and shown as resolved by the
Marketplace, and `sync --strict` fails when one is unmet. A program on
PATH is none of those. A missing program never blocks, also under
`--strict`, because the PATH agent-marketplace sees can differ from the
host's (GUI launches, launchd, IDE terminals). Dependency binaries are
never executed; dependency versions, libraries, OS packages and package
managers are out of scope.

### 9. "Known to work" evidence

Profiles carry `verified` ranges with a date and evidence reference,
following the shipped-catalog evidence pattern. Outside the range, sync
still works and `status`/TUI warn from the last recorded probe
("herdr-app 0.11 is newer than this profile's verified range").

Packages carry the same kind of evidence in their sidecar. `host.min` is
the oldest host version the package works on and `host.tested` the last
one someone tried it on (Decision 8).

A package that started as a copy of a mod or layout the host ships
records `cloned_from` with the shipped name and host version, as the
Herdr app does for its own clones. The host never updates a clone, so
package views show the origin and note when the installed host is newer
than the copy.

### 10. Where profiles come from

Origin and distribution are separate. A profile is a virtual resource of
kind `target-profile` and name `<id>`: like a collection it is selected
and locked, and it installs no files of its own. It can originate in agent-marketplace
(the built-in source), the upstream app's repository (preferred end state:
the profile versions with the app), an internal team's repository, or a
local directory while authoring. It is distributed through any configured
Git, directory or Marketplace source, pinned in the lock like any
resource, and resolved before the resources that need it.

People writing their own profiles is a main goal, so external profiles
are supported from the first profile change, not added later.

**Enabling a target** is one action: selecting its profile. Selection
appends the profile's id to the selecting layer's `targets:` list, and
deselecting the profile removes it. The two can still be edited apart by
hand:

- An id in `targets:` without its profile selected is a configuration
  error (Decision 2).
- A selected profile whose id was removed from `targets:` leaves the
  target disabled; its packages are skipped and `status` says so.

A profile with only a user root takes effect only when selected in the
user or organization layer.

Any configured source may supply profiles. Existing `allowed_registries`
and `allowed_marketplaces` policies restrict sources for organizations
that want only curated profiles; the Marketplace then also acts as the
discovery surface for every profile it serves. Local-directory profiles are
flagged as unvetted in `status`. Approval per revision (Decision 7) is the
gate for every profile, whatever its source.

### 11. Presentation (sketch, not yet discussed in depth)

- Group by destination: today's agent resource view stays as is; each
  enabled target with host kinds gets its own TUI section and web catalog
  tab/facet. Disabled targets are hidden in the TUI and filterable on the
  web.
- Host packages render through one generic package card driven by profile
  `display` metadata (name, description, preview, capability summary,
  owned keys). Adding a target adds no per-target UI code.
- Host kinds are target-scoped names (`herdr-app/mod`), so the agent kind
  list does not grow.
- Package views separate placement and verification results from pending
  manual configuration or activation. A placed package with outstanding
  setup or an unmet package requirement must not be presented as fully
  ready to use. An empty `manual_setup` or successful host check alone
  does not establish readiness (Decision 6).
- Package requirements reuse the "Requires" and "Required by" sections
  that the prerequisite stack adds to the TUI and the Marketplace
  resource page (Decision 4).
- The capability summary is labeled as analysis output, not as a
  permission the host enforces (Decision 7).

### 12. Author dry-run and live verification

Mod authors need a way to check a local source and its import declaration
before submitting it or changing their installed setup. The author
dry-run uses the same importer, profile validation and placement planner
as Marketplace ingestion and client sync. It works without an
organization layer or a running host. CLI spelling is fixed in OpenSpec.

The report shows discovered package identities, selected and excluded
source files, native destination paths, profile revision, invalid or
escaping paths, missing declared packaged dependencies, declared package
requirements, names the host ships, and pending manual setup. When given
a destination, it also reports collisions and proposed ownership changes
against current state without applying them; otherwise it marks
destination checks as not performed. A refused import returns nonzero
with file-specific diagnostics.

Dry-run writes no target files, consumer configuration, acquisition locks
or approval/ownership state. It executes no host commands, mod code,
version probes or upstream installers. Source acquisition may use the
ordinary cache when the author chooses a remote source; a local source
needs no network acquisition.

Explicit live verification is separate and follows Decision 6's approval,
binary binding and execution rules. It exercises the profile's check
against an explicitly selected installed package; it does not implicitly
install, activate or repair that package. Producing `verified` evidence
must record which profile/package revisions and host version were checked.

Source containment, declared dependencies and destination permissions
are enforceable checks. Static analysis can identify some missing
references or placeholders but cannot prove every path constructed by
JavaScript or Python at runtime. The report distinguishes checked,
refused and untested behavior; successful dry-run is not a claim of
runtime correctness. Known incomplete packages such as the queue and
links examples need author fixes before acceptance, even if a generic
static checker cannot discover every defect.

### 13. Profile identity, discovery and assessment binding

This is a **proposed design**, not supported behavior or a wire-format
definition. No production parser, schema, API, artifact, assessment, lock,
or lifecycle implementation changes in this decision.

#### Current contracts and implementation seams reviewed

The current specifications and code establish these boundaries; they do
not implement the host-package proposal below:

| Current boundary | Evidence at this worktree's HEAD |
|---|---|
| Compiled harness-target IDs and selectable Marketplace kinds are separate registered vocabularies. Current compiled IDs are `claude`, `factory`, `cursor`, `windsurf`, `gemini`, `devin`, `codex`, and `opencode`; current Marketplace kinds are the five leaf kinds plus virtual `collection`. | `openspec/specs/marketplace-api/spec.md` (Capabilities document and wire vocabulary); `pkg/harness-compilation/target/target.go`; `pkg/marketplace/api/kinds.go`; `internal/config/targets.go` |
| Current `kind/name` and `registry/kind/name` CLI locators split on literal slashes and accept only the fixed v4 kind set; `parseV4Locator` and `validateV4Exclusion` implement locator and exclusion parsing. Names reject path separators, controls, dot components, and selected wildcard/whitespace cases. The Marketplace route parser also rejects `%`, reserves `@` for an exact digest, and has a stricter installed-filename check. Browser locators pass through `parseBrowserLocator` and `hasUnsafeURLPath`, which currently rejects escaped `%2F` before splitting the decoded path. Marketplace kinds are a static allowlist, not arbitrary slash names. | `internal/config/v4.go`; `internal/marketplace/locator.go`; `pkg/marketplace/nameutil/name.go`; `pkg/marketplace/api/kinds.go` |
| `kind:name` prerequisite shorthand splits at the first colon, then accepts the existing kind aliases; unknown or unsupported kinds are skipped with `catalog.requires.invalid`. Structured prerequisites carry separate kind and name fields. | `openspec/specs/resource-prerequisites/spec.md`; `internal/catalog/catalog.go`; `internal/resolver/resolver.go` |
| Resource assessment digests are canonical SHA-256 identities over assessment fields, and the client validates that digest and its coordinate/content-digest binding. The digest is not a cryptographic signature. Artifact content/archive digests are separately verified. The server serves stored revisions and assessments through the Marketplace API. | `openspec/specs/marketplace-analysis/spec.md`; `openspec/specs/marketplace-publication/spec.md`; `openspec/specs/marketplace-api/spec.md`; `pkg/marketplace/store/assessment.go`; `internal/marketplace/client.go`; `pkg/marketplace/api/server.go`; `pkg/harness-compilation/artifact/canonical.go` |
| Ingestion currently chooses the configured registry policy and creates resource revisions, inspection manifests and assessments; the serving command wires the API and managed worker. It has no host-package profile-selection setting. | `pkg/marketplace/ingest/ingest.go`; `cmd/agent-marketplace/serve.go`; `cmd/agent-marketplace/serve_managed.go`; `pkg/marketplace/config/config.go` |
| Marketplace cache metadata stores assessed revisions; resolver paths validate revision/assessment and artifact identities. Existing lock records carry typed kind/name, content and archive digests, and index-only occurrence evidence; they do not carry profile identity. | `internal/marketplace/cache.go`; `internal/marketplace/resolver.go`; `internal/marketplace/lock_evidence.go`; `internal/marketplace/pin_artifact.go`; `internal/marketplacelock/lock.go`; `internal/marketplacelock/merge.go`; `internal/lockfile/schema.go`; `internal/lockfile/marketplace.go` |
| Exact source revisions, registry identities, discovery, source loading and revocation have existing separate seams. `registryid` currently sanitizes telemetry; it is not a profile identity authority. | `openspec/specs/acquisition-lock-codec/spec.md`; `openspec/specs/deployed-acquisition-locks/spec.md`; `openspec/specs/mutable-ref-cache/spec.md`; `openspec/specs/exact-snapshot-verification/spec.md`; `openspec/specs/catalog-discovery/spec.md`; `openspec/specs/directory-source-discovery/spec.md`; `openspec/specs/resource-listing/spec.md`; `openspec/specs/resource-selection/spec.md`; `openspec/specs/plugin-packages/spec.md`; `openspec/specs/plugin-delivery/spec.md`; `openspec/specs/marketplace-delivery-security/spec.md`; `internal/sourcerevision/builder.go`; `internal/registryid/registryid.go`; `internal/catalog/catalog.go`; `internal/resolver/resolver.go`; `internal/config/v4.go`; `internal/marketplace/discovery.go`; `internal/marketplace/revocation.go` |
| The current analyzer has shared text scanners and warning/reject policy, and rejects unknown non-UTF-8 support files as `marketplace.find.invalid-utf8`; its narrow recognized-binary exemptions do not include executable signatures. | `openspec/specs/marketplace-analysis/spec.md`; `pkg/marketplace/analyze/scanners.go`; `pkg/marketplace/analyze/policy.go`; `pkg/marketplace/analyze/codes.md` |

Current v4 locator kinds are `skill`, `command`, `subagent`,
`hook`, `mcp`, `collection`, and `plugin`, while Marketplace API/route
kinds are exactly `skill`, `command`, `subagent`, `hook`, `mcpserver`,
and `collection`.
`v4NameRE` allows any nonempty character sequence except slash,
backslash, and ASCII controls; the locator also rejects `.`/`..`,
`*`, and `?`, while `nameutil.ValidateSegment` requires a clean,
nonempty single segment. Other Unicode and punctuation (including `:`,
`@`, and `%`) can pass those configuration name checks. The
filename-facing Marketplace name check also rejects
Windows-invalid punctuation, trailing dots/spaces and DOS device
names. Current `kind:name` prerequisite parsing also rejects `all` and
`collection`.

#### Profile identity, selection and removal

**Proposed profile identity:** keep `target_id` matching
`[a-z][a-z0-9-]{0,63}` and distinct from existing compiled **harness
target** IDs. The profile loader reserves every ID
returned by the compiled target registry (`target.Targets()`); a profile
that declares any of those IDs is invalid and refuses before acquisition
with `marketplace.profile.compiled-target-id`. `herdr-app` is a proposed
application target ID, not a compiled harness target.

The proposed profile identity is `(target_id, source_id, revision_digest)`.
Canonical typed `source_id` forms are `builtin:agent-marketplace`;
`marketplace:<endpoint>#<instance-id>/<registry-id>/<profile-root>`;
`git:<origin>@<ref>#<source-path>`; and `directory:<canonical-root>`.
For `git:` profiles, the lock pins the resolved commit; `<ref>` is a lookup
selector, not the resolved source revision.
`<endpoint>` is the normalized value from
`internal/marketplace/locator.go:NormalizeMarketplaceURL`; credential-free
`<origin>` is the canonical URL from
`internal/lockfile/giturl.go:CanonicalGitURLV1`; `<canonical-root>` is
absolute and symlink-resolved. Source aliases are not identity.

Encode each variable component's UTF-8 bytes without Unicode normalization.
Leave only RFC 3986 unreserved bytes (`A-Z`, `a-z`, `0-9`, `-`, `.`, `_`,
`~`) literal; percent-encode every other byte with uppercase hex. Escape
`@`, `#`, `/`, `:`, and `%` inside components; grammar separators remain
literal. Split on literal separators before decoding each component once;
require canonical spelling or refuse with
`marketplace.profile.identity-invalid`. Examples: `builtin:agent-marketplace`;
`marketplace:https%3A%2F%2Fmp.example#mp-1/curated/profiles`;
`git:https%3A%2F%2Fgit.example%2Fteam%2Fprofiles.git@refs%2Fheads%2Fmain#profiles%2Fherdr-app`;
`directory:%2Fsrv%2Fprofiles%2Fherdr-app`.
`revision_digest` is the exact full content digest of the selected
resource with `kind: target-profile` and `name: <target_id>`;
`profile_digest` below equals that value. The digest covers the whole
profile, including evidence-date changes; there is no narrower
delivery-only profile digest in v1.

Built-in and upstream profiles may both be discoverable candidates for
`herdr-app`. A source-bound selection names exactly one candidate; there
is no precedence rule that silently chooses one, no implicit latest, and
no fallback if its source or pinned revision is absent. If two distinct
profile identities for one `target_id` are selected in the same or
different layers, resolution refuses before acquiring either, naming both
source identities and declaring layers with
`marketplace.profile.selection-ambiguous`. Repeated selections of the same
identity collapse to one. Switching explicitly from the built-in profile
to an upstream profile changes identity even if their profile bytes are
equal; it requires fresh local approval and a matching package
reassessment before Marketplace delivery.

A deselected profile with packages still directly selected triggers
Decision 1's target-scoped configuration error
(`marketplace.profile.deselected-package`) before acquisition. If owned
files exist, retain bytes and ownership; refuse target-specific sync,
update, prune, or cleanup that would mutate them, while unrelated targets
proceed. If the profile remains selected but its exact source revision
disappears, report it unavailable (`marketplace.profile.unavailable`, not a
deselection/configuration error) and apply the same retention and refusal.
Explicit retirement requires the exact profile identity/revision, approved
command set, and recorded resolved root/executable and placement; if
unavailable, refuse cleanup.
A different profile revision never authorizes pruning old output. This
defines retention only, not register lifecycle or garbage collection.

#### Package boundary, metadata and one kind grammar

**Proposed package boundary:** each package is one registry entry with the
typed identity `(target_id, kind, name)`. Its registry directory is
`packages/<target-id>/<local-kind>/<name>/`; the directory contains
`agent-marketplace.yaml` and a `payload/` tree. The sidecar is the sole
registry-side location for import mapping and identity-relevant metadata.
`files` lists each imported file explicitly as `{source_path,
destination_path}`.
`source_path` is relative to `payload/`; `destination_path` is relative to
the profile-resolved target root. Tree imports expand to explicit file
records before ingestion. Every payload file maps exactly once; source paths
use `/`, have no empty, `.` or `..` components, stay below `payload/`, and
resolve to regular nonsymlink files. Unmapped files and duplicate source
mappings refuse. Each destination is relative, unique, and contained beneath
an entry in the profile's `into` allowlist; escapes and duplicate destinations
refuse before digesting. `display_files: array<string>` lists sidecar-relative
display files, not payload, and remains under the package root.

The proposed sidecar identity fields are `target_id: string`,
`kind: string`, `name: string`, and `files: array<{source_path: string,
destination_path: string}>`; `display_files` and provenance fields, including owner, remain
descriptive and are not identity authority. For example:

```yaml
target_id: herdr-app
kind: herdr-app/mod
name: chris-links
files:
  - {source_path: mods/chris-links/main.js, destination_path: mods/chris-links/main.js}
```

Use one canonical identity everywhere: `kind` is an existing unscoped kind
or exactly `<target-id>/<local-kind>`; each `target_id` and `local-kind`
matches `[a-z][a-z0-9-]{0,63}`, and scoped prefixes equal the profile ID.
`target-profile` is a reserved unscoped virtual kind named by the target ID
and exempt from the prefix rule; `collection` is virtual and `all` is not.
Host-package names use `[a-z0-9][a-z0-9._-]{0,127}`; this narrower grammar
excludes slash, backslash, colon, `@`, `%`, whitespace, controls and dot
traversal. Server ingestion uses `marketplace.package.name-invalid`;
Marketplace API/CLI locators, config/selection, prerequisites, index entries,
and online/offline lock/cache reads use `marketplace.locator.invalid-name`.

Keep existing simple-kind locator spelling. For target-scoped kinds,
encode the one kind-internal slash as the single uppercase escape `%2F`
inside the kind token, decode it exactly once, and reject all other
percent escapes. Both the CLI locator parser and Marketplace API route
parser must split the escaped path before decoding that token. The
browser-locator proposal changes `parseBrowserLocator` and
`hasUnsafeURLPath` in `internal/marketplace/locator.go`, which today refuse
`%2F`. Canonical identity
fields in `list`, the Marketplace index/API, ingestion sidecar
and acquisition lock use the unescaped `herdr-app/mod`; the lock keeps
`kind` and `name` as separate fields. The `%2F` route form requires
proxies to forward encoded slashes undecoded; verify that with the
existing reverse-proxy check before implementation. Examples:

| Surface | Proposed spelling |
|---|---|
| Prerequisite shorthand | `herdr-app/mod:chris-links` |
| Structured prerequisite | `{kind: herdr-app/mod, name: chris-links}` |
| Consumer config `resources:` shorthand (unqualified) | `herdr-app%2Fmod/chris-links` (`internal/config/v4.go:parseV4Locator`) |
| Consumer config `resources:` shorthand (registry-qualified) | `curated/herdr-app%2Fmod/chris-links` (`internal/config/v4.go:parseV4Locator`) |
| Consumer config structured `resources:` entry | `{kind: herdr-app/mod, name: chris-links}` |
| Consumer config `exclude:` kind wildcard | `herdr-app%2Fmod/*` (`internal/config/v4.go:validateV4Exclusion`) |
| Unqualified CLI locator | `herdr-app%2Fmod/chris-links` |
| Registry-qualified CLI locator | `curated/herdr-app%2Fmod/chris-links` |
| Marketplace locator | `mp:curated/herdr-app%2Fmod/chris-links@sha256:<64-hex>` |
| Browser locator | `https://mp.example/r/curated/herdr-app%2Fmod/chris-links/revisions/sha256:<64-hex>` |
| List, index and lock identity | `{kind: herdr-app/mod, name: chris-links}` |
| Profile locator and lock record | `target-profile/herdr-app`; `{kind: target-profile, name: herdr-app}` |

Today the v4 parser reads `herdr-app/mod/chris-links` as registry
`herdr-app`, kind `mod`, name `chris-links`, then refuses unknown kind
`mod`. The proposal keeps that refusal without slash-kind reinterpretation,
using `marketplace.locator.ambiguous-kind` and suggesting
`herdr-app%2Fmod/chris-links`. Lowercase `%2f`, double decoding, or
malformed percent escapes refuse with
`marketplace.locator.invalid-encoding`; extra slashes or an unescaped slash
inside the kind refuse with `marketplace.locator.ambiguous-kind`. The
consumer-config locator and exclusion seams, `parseV4Locator` and
`validateV4Exclusion`, apply the same proposed refusal codes; today they
handle only the fixed-kind forms. The existing prerequisite parser splits
`kind:name` at the first colon, so
the proposed slash-kind form is unambiguous because `kind` excludes
colons; the new kind must be accepted in both structured and shorthand
forms rather than silently dropped by `catalog.requires.invalid`.

The new API capability and OpenAPI vocabulary must advertise the exact kind.
The current client fails closed on unknown selectable kinds
(`plugin-packages`); no fallback is proposed.
Layer reads decode in `internal/dependencytxn/layer_lock.go`;
`internal/lockfile/codec.go` normalizes via
`internal/lockfile/marketplace.go:normalizeMarketplaceResource`, which
rejects kinds outside `api.IsMarketplaceKind`. Released v4.1.4 therefore
rejects `herdr-app/mod` and kind `target-profile` as unknown before
acquisition and does not reinterpret them as another kind or path;
`internal/marketplacelock/merge.go` receives only decoded records and
never ignores or reinterprets them. Host targets have no repository
roots, so these records belong only in user/organization locks;
repository-committed locks contain none and remain readable by v4.1.4.

#### Proposed authenticated package/profile binding

The sidecar may contain `content_digest`, `mapping_digest`, or other hash
claims, but they are unauthenticated input and never an assessment.
Ingestion recomputes every identity value from the payload, file map and
the exact selected profile.

Add the same proposed `host_package_identity` object to the Marketplace
`ResourceAssessment` and canonical artifact manifest. Its exact fields
and types are:

| Field | Type | Computation |
|---|---|---|
| `package_content_digest` | `string`, `sha256:<64 lowercase hex>` | SHA-256 of the UTF-8 domain-tag bytes `agent-marketplace/host-package-content/v1`, then `0x00`, then records sorted by the UTF-8 bytes of `path`. `path` is the UTF-8 bytes of payload-relative `source_path`, uses `/`, has no empty, `.` or `..` components, and is not NFC-normalized. `mode` is one byte: `0x00` for mode `0644` or `0x01` for mode `0755`; all other modes, symlinks, and other file types refuse. Each record is `u64be(len(path)) || path || u64be(1) || mode || u64be(len(bytes)) || bytes`, where lengths count bytes and `u64be(n)` encodes unsigned 64-bit big-endian `n`. Sidecar and display-only files are excluded; the digest is independent of profile choice. |
| `delivery_mapping_digest` | `string`, `sha256:<64 lowercase hex>` | SHA-256 of the UTF-8 domain-tag bytes `agent-marketplace/host-package-mapping/v1`, then `0x00`, then records sorted by UTF-8 `source_path` bytes and then `destination_path` bytes. Each record is `u64be(len(source_path)) || source_path || u64be(len(destination_path)) || destination_path`; paths are UTF-8 bytes with `/` separators and no NFC normalization, lengths count bytes, and `u64be(n)` is unsigned 64-bit big-endian. Duplicate `destination_path` values refuse before digesting. |
| `profile_digest` | `string`, `sha256:<64 lowercase hex>` | The exact full `target-profile` resource content digest; equal to `profile_identity.revision_digest`. |
| `profile_identity` | object `{target_id: string, source_id: string, revision_digest: string}` | `target_id` matching `[a-z][a-z0-9-]{0,63}` and canonical `source_id` identify the selected profile; `revision_digest` is its exact full content digest. |

Compiled-kind `content_digest` retains today's `artifact.ContentDigest(payload)`
SHA-256 over canonical payload bytes
(`pkg/harness-compilation/artifact/canonical.go`), consistent with the
`marketplace-api` requirement "Canonical artifact format". For host kinds
only, proposed `content_digest` instead equals the tree digest in
`package_content_digest` above.

The assessment's existing canonical
`assessment_digest` includes the complete `host_package_identity`; the
artifact manifest repeats it, and the proposed Marketplace lock record
stores that object plus `assessment_digest`. Profile resource selection
and package selection remain separate lock records.

The Marketplace server requires an explicit per-package assessment
selection keyed by registry ID, kind and name, containing the exact
`profile_identity`, for example:

```yaml
host_package_assessment_profiles:
  - registry_id: curated
    kind: herdr-app/mod
    name: chris-links
    profile_identity:
      target_id: herdr-app
      source_id: marketplace:https%3A%2F%2Fmp.example#mp-instance/curated/profiles
      revision_digest: sha256:<64-hex>
```

This is proposed configuration pseudocode, not a server schema. The server
never chooses the newest profile. Unset selection refuses
ingestion/publication with proposed
`marketplace.profile.unconfigured`; a configured source/revision mismatch
refuses with `marketplace.profile.revision-mismatch`. Updating the
configured profile requires reassessment and publication, even when
package bytes are unchanged. Until then, clients selecting the new profile
refuse the old assessment.

Verification sequence, all proposed:

1. **Ingestion** resolves the explicitly pinned profile revision, checks
    the target ID, validates the sidecar paths, computes the package and
    mapping digests from the exact source bytes, and creates the assessment
    binding. An unset profile or mismatched sidecar digest claim refuses
    with `marketplace.profile.unconfigured` or
    `marketplace.package.claim-mismatch`, respectively, and creates no
    installable assessment.
2. **Publication** checks that assessment, resource revision and artifact
   manifest carry identical identity fields; verifies the computed
   assessment/content/archive digests; and atomically publishes them
   together. A mismatch leaves the previous publication unchanged.
3. **Online resolution** validates the Marketplace assessment digest and
   revision coordinate/content digest, verifies artifact/archive bytes,
   and compares `profile_identity` and `profile_digest` with the selected,
   locked profile before staging any package files.
4. **Cached/offline delivery** requires the lock's `assessment_digest`
   and `host_package_identity`, cached assessed revision and cached
   artifact to agree, verifies their digests, then compares the binding
   with the exact locally selected profile revision. A missing or older
   cache binding refuses offline delivery; it never infers a binding from
   sidecar metadata or fetches a replacement in a no-fetch mode.

`assessment_digest` is the existing content-addressed assessment identity,
not a digital signature. The proposal adds the fields to that canonical
digest and reuses the existing Marketplace publication/API trust boundary
and artifact digest verification; it introduces no signing key or claim
that a public hash alone authenticates an assessor. Git and directory
sources cannot borrow Marketplace trust: if either declares Marketplace
assessment or binding fields, refuse it as
`marketplace.assessment.untrusted-source`.

The complete proposed refusal-code set, each with its emitting side:

| Proposed refusal | Emitting side | Proposed diagnostic |
|---|---|---|
| Server profile selection is unset | server ingestion/publication | `marketplace.profile.unconfigured` |
| Configured profile source or revision differs | server ingestion/publication | `marketplace.profile.revision-mismatch` |
| Two distinct identities are selected for one target; profile declares compiled target ID | config/selection | `marketplace.profile.selection-ambiguous`; `marketplace.profile.compiled-target-id` |
| Package remains selected after profile deselection; selected profile revision is unavailable | config/selection | `marketplace.profile.deselected-package`; `marketplace.profile.unavailable` |
| Source ID has an unsupported form or malformed/noncanonical component escapes | server ingestion/publication; client online; client offline; config/selection | `marketplace.profile.identity-invalid` |
| Profile `target_id` violates `[a-z][a-z0-9-]{0,63}` | server ingestion/publication; client online; client offline; config/selection | `marketplace.profile.identity-invalid` |
| Raw/extra slash or unknown/malformed kind | Marketplace API route; CLI locator; config/selection; client online/offline lock/cache reads | `marketplace.locator.ambiguous-kind` |
| Locator percent escapes are malformed or noncanonical | Marketplace API route; CLI locator; config/selection | `marketplace.locator.invalid-encoding` |
| Source/display path is absolute or traversing; outside-root or symlink-escaped | server ingestion/publication | `marketplace.package.path-invalid`; `marketplace.package.path-escape` |
| Sidecar kind is neither an existing unscoped kind nor valid `<target-id>/<local-kind>` | server ingestion | `marketplace.package.kind-invalid` |
| Sidecar `target_id` disagrees with the scoped `kind` prefix | server ingestion | `marketplace.package.sidecar-mismatch` |
| Artifact kind prefix disagrees with the bound profile target ID | client online; client offline | `marketplace.assessment.identity-invalid` |
| Host-package `name` violates `[a-z0-9][a-z0-9._-]{0,127}` | server ingestion/publication | `marketplace.package.name-invalid` |
| Host-kind name violates `[a-z0-9][a-z0-9._-]{0,127}` | Marketplace API/CLI locator; config/selection; prerequisite; client online/offline index, lock/cache reads | `marketplace.locator.invalid-name` |
| Prerequisite kind is unknown or malformed | prerequisite parsing | `catalog.requires.invalid` |
| Payload or display file is symlink/nonregular, or payload mode is not `0644` or `0755` | server ingestion/publication; client online; client offline | `marketplace.package.file-type-invalid` |
| Payload file is unmapped; source mapping or destination path is duplicated | server ingestion | `marketplace.package.file-unmapped`; `marketplace.package.mapping-duplicate-source`; `marketplace.package.mapping-duplicate-destination` |
| Destination path is malformed; destination is outside the profile's `into` allowlist | server ingestion/publication; client online; client offline | `marketplace.package.destination-invalid`; `marketplace.package.destination-escape` |
| Sidecar digest claim differs from recomputed value | server ingestion | `marketplace.package.claim-mismatch` |
| Assessment or artifact binding is absent | server publication; client online; client offline | `marketplace.assessment.identity-missing` |
| Binding differs from its recomputed assessment/artifact digest | server publication; client online; client offline | `marketplace.assessment.identity-invalid` |
| Selected profile source or revision differs | client online; client offline | `marketplace.profile.binding-mismatch` |
| Recomputed import mapping differs | server ingestion/publication; client online; client offline | `marketplace.package.mapping-mismatch` |
| Offline cache/lock has no profile binding | client offline | `marketplace.cache.profile-binding-missing` |
| Git/directory input claims a Marketplace assessment | server ingestion/publication; client source loading online/offline | `marketplace.assessment.untrusted-source` |
| Package bytes do not match `package_content_digest` | server ingestion/publication; client online; client offline | `marketplace.package.content-mismatch` |
| Existing analyzer rejects an opaque executable asset or unknown non-UTF-8 support file | server ingestion/analyzer | `marketplace.find.executable-asset`; `marketplace.find.invalid-utf8` |

#### Proposed analysis disclosures and limits

The host-package analyzer derives three disclosures from package bytes and
the explicit file map: **exec** (program/process creation or executable
scripts), **network** (network API calls or URL/network client references),
and **external-file access** (reads/imports outside the package payload or
absolute, parent-relative, or `file:` paths). Each result is labeled
**analysis output, not permission** and carries the file, line, analyzer
version, and one of `signal found`, `no static signal found`, or
`analysis unavailable`; absence of a signal is not proof of absence.

Proposed language signals are bounded:

- **gpui-kit JavaScript:** parse supported JavaScript/TypeScript syntax and
  inspect static imports plus call sites. Flag process APIs such as
  `child_process`, `Bun.spawn` and `Deno.Command`; network APIs such as
  `fetch`, `WebSocket`, and `http`/`https`; and filesystem APIs/imports
  such as `fs`, `node:fs` and `Deno.readFile`, including statically
  resolvable paths outside the package.
- **Luau:** inspect parsed calls and literal module/path arguments for
  `require`, `loadstring`, `HttpService:GetAsync`/`PostAsync`,
  `request`/`http_request`, `io.open`, `os.execute`/`io.popen`, and
  absolute or parent-relative paths where those APIs are present in the
  declared host runtime. A host API not in the analyzer's versioned signal
  table is reported as unavailable, not assumed safe.
- **Python and shell helpers:** scan every declared helper file rather
  than only the entry point. Python signals include `subprocess`,
  `os.system`/`popen`, `socket`, HTTP clients, `open`/`pathlib`, and
  imports resolving outside the package. Shell signals include commands
  such as `exec`, `curl`/`wget`, `source`/`.` and redirections or paths
  outside the package. Helpers are analyzed as data and never executed.

Computed arguments cannot reliably identify the eventual executable,
URL or path. `sh -c` is disclosed as exec with an unknown command, not
proved safe by parsing its string. Dynamic `import`, `eval`, generated
code, indirect calls and language features the parser cannot understand
produce an unknown/unavailable disclosure and can hide dependencies.
Only packaged helper scripts are recursively scanned; undeclared external
helpers are not followed. Static analysis can miss behavior and cannot
prove all runtime dependencies.

Proposed v1 keeps the existing `marketplace.find.executable-asset`
rejection for opaque native, packaged, or binary executable assets,
including ELF, Mach-O, and PE signatures. It adds no executable-mode
rejection rule and keeps `marketplace.find.invalid-utf8`: that finding rejects
unknown non-UTF-8 support bytes, and only the current explicitly
recognized non-executable binary signatures remain exempt from text
scanning. Executable scripts remain analyzable disclosures, not
permissions. None of these summaries is host-enforced permission or a
runtime safety certificate.

Apply the same proposed disclosure policy to host `place` and `register`
package payloads. Because `herdr plugin link` skips `[[build]]`, such a
curated package must already contain its ready-to-run output; analysis and
sync never invoke a package build step.

#### Trace to future acceptance witnesses

Every row below is **proposed / not implemented**. The cited existing
specifications are source contracts or precedents, not proof that the
host-package behavior already exists.

| Proposed extension | Existing spec(s) | Implementation seam(s) | Future acceptance witness |
|---|---|---|---|
| One explicitly chosen profile identity; compiled-ID and multi-layer ambiguity refusal | `resource-selection` (Name collisions resolve per install scope); `marketplace-api` (Capabilities document and wire vocabulary) | `pkg/harness-compilation/target/target.go`; `internal/config/targets.go`; `internal/resolver/resolver.go` | Built-in and upstream candidates share an ID; selecting both in one or different layers refuses before acquisition and names both; selecting a compiled ID refuses. |
| Deselect/remove keeps owned files and permits no prune under a different profile revision | `plugin-delivery` (Removal retires only owned plugin output); `exact-snapshot-verification` (Exact snapshots are scoped to the configured root) | `internal/ownership/state.go`; `internal/materialize/materialize.go`; `internal/marketplace/resolver.go`; `internal/config/` | Remove the selected profile with owned files: bytes and ownership remain; an unavailable exact source revision reports unavailable and retains them; cleanup with a different profile revision refuses. |
| Same package bytes under a different selected profile refuse online and offline | `marketplace-api` (Canonical artifact format); `marketplace-publication`; `resource-listing` (List includes Marketplace selections from the verified cache) | `pkg/marketplace/store/assessment.go`; `internal/marketplace/client.go`; `internal/marketplace/resolver.go`; `internal/marketplace/cache.go` | Keep package content digest constant, change selected profile identity, and assert refusal before materialization in online and no-fetch offline modes. |
| Explicit server profile pin; unset or changed profile blocks assessment until reassessment | `marketplace-publication`; `marketplace-api` | `pkg/marketplace/config/config.go`; `pkg/marketplace/ingest/ingest.go`; `cmd/agent-marketplace/serve_managed.go`; `pkg/marketplace/store/assessment.go` | Ingestion without a configured exact profile refuses; changing the configured profile digest refuses publication until the package is reassessed and the new identity is published. |
| Offline cache reuse only with a valid locked binding; tampered/missing binding refuses | `resource-listing` (List includes Marketplace selections from the verified cache); `deployed-acquisition-locks` (Deployed evidence is validated before use); `acquisition-lock-codec` (Acquisition lock encoding is deterministic and canonical) | `internal/marketplace/cache.go`; `internal/marketplace/lock_evidence.go`; `internal/lockfile/schema.go`; `internal/marketplacelock/lock.go` | A valid profile-bound cache resolves offline; tampering with the cached assessment/artifact binding or removing the binding fails closed without network access. |
| Mapping digest mismatch and Git/directory Marketplace-assessment claims refuse | `marketplace-api` (Canonical artifact format) | `pkg/marketplace/ingest/ingest.go`; `pkg/marketplace/store/assessment.go`; `internal/marketplace/resolver.go`; `internal/catalog/catalog.go` | Alter the mapping after assessment, or put a Marketplace assessment claim in Git/directory metadata; both refuse before staging and name the affected identity. |
| Package boundary and file-map containment refuse unsafe imports | `registry-symlink-containment` (Configured source root bounds symlink validation; Disk primary payload ingestion stays below captured source authority) | `pkg/marketplace/ingest/ingest.go:registryEntryPath`; `pkg/marketplace/ingest/ingest.go:fileSetForResource`; `internal/fsboundary.OpenSourceFS`; `internal/fsboundary.Dir.OpenPath` | Out-of-root paths, symlink escapes, nonregular files, unmapped payload files, duplicate source mappings or destinations, and destinations outside profile `into` all refuse before digesting or staging. |
| Slash-kind locator round-trip through list, Marketplace index, ingestion and lock | `marketplace-api` (Capabilities document and wire vocabulary → Selectable resource kinds); `resource-prerequisites` (Prerequisite kind syntax); `plugin-packages` | `internal/marketplace/locator.go`; `internal/catalog/catalog.go`; `pkg/marketplace/api/server.go`; `pkg/marketplace/ingest/ingest.go`; `internal/lockfile/marketplace.go` | `herdr-app/mod` round-trips as one kind and `chris-links` as name through both locator forms, list/index JSON, ingestion, and lock read/write. |
| Ambiguous slash-kind and duplicate profile selection refuse before acquisition | `catalog-discovery`; `resource-selection` (Name collisions resolve per install scope) | `internal/marketplace/locator.go`; `internal/config/v4.go`; `internal/resolver/resolver.go` | Raw `herdr-app/mod/chris-links`, malformed percent escapes, and two selected profile identities produce deterministic named refusals with no source acquisition. |
| Host-package disclosure and binary policy | `marketplace-analysis` (Non-UTF-8 support files cannot silently bypass analysis; Binary signature exemptions remain narrow); `marketplace-delivery-security` | `pkg/marketplace/analyze/assets.go:ScanAssets`; `pkg/marketplace/analyze/analyzer.go:isRecognizedBinary`; `pkg/marketplace/ingest/ingest.go` | Fixtures cover JavaScript, Luau, Python/shell helpers, computed commands, dynamic code and prebuilt binaries; output is labeled disclosure and never permission. |

### 14. Place publication, ownership and recovery

This decision is **proposed, not implemented**. It settles place delivery
only; it does not add register-mode execution, an external-command rollback,
or a second ownership database.

#### Required extensions

- `dependencytxn.ClassifyDestination` currently manages only `.agents`
  config/lock leaves; other paths take `AuthorityLegacy`, whose `stageFile`
  creates `.dependencytxn-*.tmp` beside the destination, `publish` uses
  path-based `os.Rename`/`os.Remove`, and `classify` uses
  `inspectDestination`. Place destinations require a new authority branch in
  `commitLocked`, `publish` and `classify` that binds the captured root and
  publishes agent-owned staging through `Destination.Publish`; place must
  never use `AuthorityLegacy` or create dot-prefixed host staging siblings.
- `validateStagingPath` and `validateJournal` currently require a stage to
  remain contained in the destination parent (directly in it for managed
  entries) and require strictly ascending file paths. A place intent must
  remain a `.dependencytxn-*.json` journal visible to `CheckPending` and
  `ReadPendingPluginGenerationRefs`, accept a journaled agent-owned stage
  outside the destination parent, and replace that path-order invariant with
  an explicit dependency-topological publish order (path-order ties). A
  pending journal keeps today's behavior, where `Commit` refuses
  destinations that overlap a pending journal and callers of `CheckPending`
  refuse while any journal is pending.
- `stageCapturedDestination` currently uses
  `Destination.StagingDir` in the destination parent, while
  `publishLocked` uses `.packagegen-*` stages swept by
  `cleanupPackageStages`. Place requires a new agent-owned staging
  constructor and a reserved stage name outside that sweep, with the intent
  durable before the first staging write.

#### Publication units and observation limits

The place-transaction intent contains the full payload manifest, immutable
generation key, destination generations and reserved engine-stage path.
Then stage each reviewed package as a complete, immutable generation in
agent-marketplace-owned state, outside every application root and outside
the acquisition cache. Extend the existing `internal/packagegen` generation
namespace and lock/collection discipline for place identities; its current
`Plan`, `PublishUnderPackagesLock` and `GarbageCollect` are plugin-specific
and do not store Herdr packages, so place extends the seam rather than
reusing it unchanged. The place generation key includes
Decision 13's profile identity and mapping plus every payload path, mode and
content digest; extend `internal/packagegen/packagegen.go:BuildKey` and
`internal/packagegen/gc.go:GarbageCollect` to retain it while a committed
projection or pending transaction references it, extending
`internal/ownership/plugin_state.go:ReadAllPluginGenerationRefs` and
`ReadPendingPluginGenerationRefs`. Keep those references in the existing
root-keyed `internal/ownership` record, not a parallel store. The completed
generation is the recovery input. `packagegen.publishLocked` has an
existing-generation branch that calls `verifyGeneration` against the input
`PackageRoot`; place recovery never routes through that branch or reads
payloads back from an upstream checkout.

For publication, capture the approved canonical target root with
`internal/fsboundary/dir_unix.go:OpenDir`; use
`internal/fsboundary/root_unix.go:OpenContainedRoot` and
`internal/fsboundary/destination_unix.go:CaptureDestination` for contained
paths and the repository link policy. Revalidate the destination leaf and any
honored link before every publish or removal with `Destination.Verify`/
`Publish`. Those methods remain bound to the pinned root object and do not
re-resolve its path. As a required extension to place authority, re-stat the
named root path immediately before every publish or removal and require
`SameObject` with the pinned root identity; otherwise refuse with a generation
conflict, following
`internal/materialize/destination_boundary.go:captureDirectory`. The root's
own parent chain is not captured, so a concurrent move of a root ancestor
between that re-stat and the write is an observation limit, like the existing
ancestor window below.
Keep the unconditional `.packagegen-*` sweep limited to existing plugin
stages. Recovery removes a place stage only when its recorded directory
identity and manifest match. An unrecorded, missing-after-capture, changed or
foreign stage is retained with its journal and stops recovery. With the required
authority and journal extensions above, place recovery extends the
generation checks and exact old/new classification in
`internal/dependencytxn/txn.go:CheckPending`, `Recover` and `recoverJournal`;
it is not an unchanged reuse of their current destination and ordering rules.
`internal/fsboundary/destination_unix.go:Destination.Publish` provides one
descriptor-relative, same-filesystem rename; it is not a multi-file
transaction.

Choose **refusal**, not copy-then-verify, for cross-filesystem publication.
Before the first target write, compare the staging filesystem identity with
the effective target parent after any honored link, and refuse the package
set if they differ. If a rename nevertheless returns `EXDEV`, stop and retain
the staged generation and pending journal; never fall back to a target-root
temporary or copy. If no file has yet been published, the target remains
unchanged. If earlier files were published, they remain visible and recovery
follows the table below.

The conflict plan is whole-set, but publication is per file. When a required
mod is in the admitted set, publish it before its layout; deselection removes
layouts before their mods. Whether to admit a layout whose required mod is
absent remains open under Decision 4. The ordering prevents a *new* layout
from appearing before its selected required mod is published, but an old
layout may remain while its mod is updated, and a removed layout may leave
its mod visible until the later removal. A crash or running app can observe a
partially replaced package or a mixture of old and new files. There is no
host reload barrier or guaranteed reload ordering: Herdr may hot-reload
between any two renames, including while a layout or mod package is
incomplete. Do not promise package- or dependency-set atomicity, suppress
reload, or infer readiness from publication.

#### Root binding and whole-set conflicts

Decision 7's approval binds the resolved target root; Decision 14 also binds
place ownership, status and retirement to that recorded canonical root.
Reuse `internal/ownership/store.go:Open`,
`internal/ownership/paths.go:CanonicalRoot`/`RootKey` and
`internal/ownership/state.go:validateRecord`'s canonical-root checks.
Approval is not authority to follow a new
environment value at write time. A changed `HERDR_APP_HOME`, root alias or
resolved root requires a new approval and describes a separate installation;
approving it never retires or relocates the recorded installation. Retirement
is an explicit operation against the old recorded root and its authority.

Resolve and pin the root directory identity at plan admission; reuse
`internal/materialize/destination_boundary.go:captureDirectory`'s named-root
identity check as the precedent. Root binding prevents environment-,
approval- and plan-time redirection and stale-root writes. Replacements of
a captured ancestor or a destination symlink are detected and
refused by `Destination.Verify` when present at that verification step: do
not retry through the new name, write, prune, or clear the journal. After
`Destination.Publish`'s `verifyTarget` returns the parent descriptor and
before `unix.Renameat`, a concurrent move of a destination ancestor carries
that descriptor with the directory object, so the write can land in an object
no longer at the approved path; this is not detected or reported. A file or
symlink leaf replaced after leaf verification is replaced at the captured
parent; a directory leaf makes `renameat` fail rather than being overwritten.
These local-move and leaf windows are observation limits, not a proposal for
new rename flags, and root binding does not prevent a concurrent local move
in that window. An alias that cannot be proven to be the recorded canonical
root is not ownership proof. A case-variant spelling on a case-insensitive
host is also an unproven alias because `ownership.CanonicalRoot` does not
fold case. Store the resolved XDG state root used
by the existing ownership store in the local approval binding. If
`XDG_STATE_HOME` changes and that recorded metadata is unavailable, report
ownership as unavailable and refuse place mutation or retirement; do not
open a new empty store and reinterpret existing files as
a first installation.

Plan the complete desired package set before any target publication. Record
in the place journal a deterministic dependency-topological publish order
(path order breaks ties), not selection or iteration order. Retain the
complete installed projection when a selected package conflicts with another
owner; refuse its new revision as a unit. Refuse every new package in a
new/new conflict, independent of selection or iteration order. Any collision
with an unowned existing file refuses the package even when its bytes match;
this whole-package refusal is a deliberate place-unit policy, unlike the
aggregate per-entry refusal; there is no implicit adoption. An installed
package deselected in the same plan still owns its paths until its prune
completes, so a new package claiming any of them is refused for that run.
Compare normalized path components as a trie: a file `mods/a` conflicts
with another package's directory `mods/a/` or any descendant. For each
component, form the collision key by NFD normalization, Unicode case-folding,
then NFC normalization on **all** hosts, following
`internal/materialize/claude_preflight.go` and
`internal/harness/claude_git_boundary.go`. This intentionally refuses some
names on case-sensitive volumes while making case and normalization
collisions deterministic. A single conflict blocks each new package's full
projection; independent packages may proceed.

These rules extend the planning precedents in
`internal/materialize/plugin_instance.go:preflightPluginInstances` (which
retains a failed plugin instance) and
`internal/materialize/plugin_batch.go:planPluginArtifacts` (which checks
prior output digests), and the
batch/ownership commit in
`internal/materialize/plugin_batch.go:pluginInstanceBatch.commit`; they are
not current Herdr package behavior. The source contracts are
`openspec/specs/plugin-delivery/spec.md`,
`openspec/specs/materialization/spec.md`,
`openspec/specs/owned-entry-reconciliation/spec.md`,
`openspec/specs/resource-removal/spec.md`,
`openspec/specs/backup-recovery/spec.md`,
`openspec/specs/exact-snapshot-verification/spec.md`,
`openspec/specs/registry-symlink-containment/spec.md` and
`openspec/specs/status-reporting/spec.md`. They distinguish
unproven collisions from proven-owned drift (`owned-entry-reconciliation`,
`Unproven entries are preserved` and
`Proven owned entries reconcile against recorded state`).

#### Recovery state machine

Keep one pending transaction from the first staging write through ownership
commit and pruning. Read-only `status`, `sync --check` and similar
commands check it first and return the existing
`dependency.recovery-required` behavior without replay. A mutating sync
recovers only the admitted transaction from its journal and recorded
generations before validating or planning fresh work. `dependencytxn`
has journal recovery that refuses unexpected bytes and non-prefix
publication; the place branch must retain those exact-generation checks
while using its explicit recorded publish order rather than the current
path-order invariant. Extend
`internal/materialize/plugin_batch.go:pluginInstanceBatch.commit` and
`ownership.Store.PrepareBatch` to enlist the ownership participant, with
package files publishing first, ownership committing next, and stale files
pruned last.
`ownership.Store.Recover` handles the separate per-root ownership journal.
The ownership state-file commit is one atomic rename, so an interruption
during it resolves to the before or after row below. The E2E crash hook in
`internal/dependencytxn/fault_e2e.go` already targets stage/publish points;
future witnesses add the place ownership and prune boundaries. The proposed
states:

| Interruption point | Read-only outcome | Mutating recovery outcome |
|---|---|---|
| Before staging completes | `dependency.recovery-required`; no replay. | If the reserved stage path is absent, clear the no-write intent. Otherwise remove only a transaction-owned partial stage whose recorded directory identity and every present byte match its journaled manifest, then clear the transaction. A stage present without recorded identity, or a missing-after-capture, changed or foreign stage, is retained with the journal and recovery stops. |
| After some files publish | `dependency.recovery-required`; no replay. | Complete the recorded publish prefix only while each destination is exactly its old or intended generation and all remaining stage bytes match. An unexpected generation, byte or non-prefix state stops without deleting evidence. |
| All package files published, ownership generation not committed | `dependency.recovery-required`; no replay. | Verify every published file against the journal, then commit the matching package/root ownership generation. A mismatch stops with journal and files intact. |
| During ownership-state commit | `dependency.recovery-required`; no replay. | The single atomic state-file rename leaves either the old generation, handled by the preceding row, or the new generation, handled by the following row. |
| Ownership committed, pruning not started | `dependency.recovery-required`; no replay. | Start pruning only stale files whose current bytes and captured parent still match the recorded old generation. A mismatch stops and preserves the journal and all files. |
| Interruption during pruning | `dependency.recovery-required`; no replay. | Continue only the remaining exact prune set, then clear the journal and release unreferenced package generations. Any changed generation stops and preserves the remaining evidence. |
| No pending journal after commit and prune | Normal read-only inspection; any verification-pending or failed result remains visible. | No recovery work; a later sync plans from the committed ownership generation. |

The journal must record the ordered package writes, ownership participant,
stale-file generations, stage manifest and root identity so these boundaries
are durable, not inferred from directory contents. `dependencytxn.Commit`
currently stages before journal publication and its journal recovery rolls
forward an exact published prefix; the proposed early journal and prune phase
are extensions to that same seam. Recovery never guesses ownership from
matching content and never deletes a stage, target or journal whose generation
differs from its recorded expectation. The journal remains pending on an
unexpected generation so a later read-only command cannot report a clean
installation.

`internal/backup/backup.go:Create` and explicit `Restore` remain user
recovery aids, not transaction rollback. A backup does not authorize
ownership or prove that a host registration can be rolled back.

#### Edited files and verification

Place payloads differ from owned aggregate entries: sync keeps an
edited file and records edited drift; if any member of an
installed package is edited, keep that package's entire current projection
and do not partly refresh it. An upgrade that encounters an edit
keeps the complete old projection and refuses the new package revision as a
unit. The user can restore or move the edit and sync again. On deselection,
delete unchanged files only when their recorded ownership generation still
matches; leave edited files byte-for-byte in place and cede those paths from
ownership, reporting them as retained user files. Neither matching bytes at
an unowned path nor a prior package name is sufficient to adopt them. This
choice follows materialization's edited-copy preservation and
`plugin-delivery`'s rule to retire only owned output; it deliberately does
not apply the aggregate-entry rule that restores drifted proven entries.

Once publication, ownership commit and pruning finish, run the approved
initial-load `verify`; record pass/fail against the exact profile, package
generation, check identity and host version in a single ownership-state
update after the journal clears. An interruption before that update leaves
verification pending, as in the post-commit row.
A failed check retains the complete placed projection and generation
reference, reports the check as failed, and does not roll files back. The
next mutating sync retries the check against unchanged files; a later user
edit is separately reported as edited drift and is not converted into a
verification failure. A crash-recovered publication is neither a failed
check nor a user edit: recovery itself runs no host code and leaves
verification pending; the sync may run its normal post-recovery check only
after completing the journal and revalidating selection, otherwise the next
mutating sync runs it. Default read-only commands (`sync --check`, `status`,
TUI drift views) run no host code and report the recorded result;
`sync --check --verify` and `doctor` run verify live under Decision 6 and
never write placement or ownership. This preserves Decision 6's distinction
among files placed, check scope, manual setup and runtime evidence. A
successful initial-load check still does not prove deferred runtime behavior.

#### Trace to future acceptance witnesses

Every row is **proposed, not implemented**. Existing specifications and code
below are precedents, not proof of Herdr place behavior.

| Proposed transition | Existing contract/seam | Future acceptance witness |
|---|---|---|
| Engine staging and same-filesystem publish | `plugin-delivery` (Package roots resolve to immutable package generations); `packagegen.Plan` / `PublishUnderPackagesLock`; `fsboundary.Destination.Publish` | Stage outside a scratch target root and assert no host sibling; a different-device target refuses before writes; injected `EXDEV` leaves its journal and stage, with no copy fallback. |
| Incomplete staging recovery | `dependencytxn.CheckPending`, `Recover`, `recoverJournal` | Crash after the first stage file: read-only `sync --check` returns `dependency.recovery-required` without byte changes; mutating recovery clears only an exact attempt-owned partial stage. Replace one stage byte and prove recovery stops with it and the journal intact. A mutating sync selecting a plugin package stops on an unrecorded or changed place stage; the stage and journal are preserved. |
| Partial publication recovery | `plugin-delivery` (Plugin instances install as a unit; Delivery transitions never activate a plugin twice); `dependencytxn.recoverJournal` | Crash after a published prefix: read-only status refuses without replay; mutating sync completes the exact recorded suffix. Change one destination generation and prove no subsequent file or evidence is deleted. |
| Published files before ownership commit | `owned-entry-reconciliation` (Apply retains originally admitted ownership-record generation); `pluginInstanceBatch.commit` | Crash after all target files but before the ownership participant: read-only refuses; recovery commits the matching ownership generation once, without reacquisition. |
| Ownership commit before/during prune | `plugin-delivery` (Removal retires only owned plugin output); `ownership.Store.Recover` | Crash before prune and between two removals: recovery prunes only matching stale generations; replace a remaining file and assert it and the pending journal survive. |
| Root change, alias or destination-symlink replacement | `materialization` (Harness target roots remain within their authorized scope); `fsboundary.CaptureDestination` / `Destination.Verify`; `ownership.CanonicalRoot` | Rename the approved root and recreate a directory at its path; place publish and prune refuse. Change `HERDR_APP_HOME`, XDG state root, a root alias or a destination symlink between plan and write: no redirected write/prune; new-root approval leaves the old root untouched. |
| Destination leaf or ancestor move after verification | `fsboundary.Destination.Verify` / `Destination.Publish` (`unix.Renameat`) | Replace the leaf or move its ancestor after `verifyTarget` returns the parent descriptor but before `renameat`: leaf replacement does not follow the leaf (a directory leaf fails), while the moved descriptor can carry publication into a directory no longer at the approved path; neither local race is detected or reported. |
| New/new, installed/new and unmanaged conflicts | `owned-entry-reconciliation` (Unproven entries are preserved; Unmanaged collisions refuse only the generated entry); `planPluginArtifacts` | Reverse package order and assert both new colliders refuse; an installed package keeps its complete old projection; an identical unowned file, file/directory prefix collision, case-fold collision and NFC/NFD twin refuse deterministically. A deselected installed owner blocks a new claimant until its prune completes. |
| Edited package on sync and upgrade | `materialization` (Locally edited file-mode output is reported as an edited managed copy); `plugin-delivery` (Plugin instances install as a unit) | Edit one package file: sync preserves the complete projection and reports drift; upgrade retains the old complete projection and refuses the new generation. |
| Edited package on deselection | `plugin-delivery` (Removal retires only owned plugin output); `resource-removal` | Deselect after editing one file: unchanged owned files retire, the edit survives byte-for-byte and is reported as ceded, with no implicit re-adoption. |
| Failed initial-load verification | `exact-snapshot-verification` (loose precedent only: snapshot trust, not placement-check results); Decision 6 verification boundary | Make the approved check fail: exact package files and ownership remain, the failure is recorded, `status` reports it without executing code, and next mutating sync retries it. |
| Crash recovery versus user edit | `status-reporting` (loose precedent only: nested-root drift, not this recovery distinction); `dependencytxn.recoverJournal` | Recover a pending write, then separately edit an installed file: recovery leaves verification pending, while the next status reports the edit rather than labeling either event as the other. |
| Host observation of dependent packages | `plugin-delivery` (Plugin instances install as a unit) | Pause between per-file renames while a host watcher reads the tree; assert only the documented partial states and dependency ordering, not an atomic view or reload guarantee. |

## Compared with the herdr-plugins proposal

The herdr-plugins proposal (Andrew Z, 2026-11-13, not built) covers
sharing for the Herdr app alone:
one internal repository, a `plugin.json` per package, a Python installer
and checks on every merge request. Its package folders have the same
shape as the curated representation in Decision 4, so that repository can
be a source for this design through import declarations.

This design stays the delivery path. It reuses the existing lock,
ownership, drift, backup and revocation machinery, delivers the bytes
that were reviewed, imports sources without changing their layout and
covers more than one app.

| From the proposal | Outcome |
|---|---|
| Shortcuts merged at install | Adopted as ordering: aggregates ship before register (Decision 5). |
| `requires` between plugins | Adopted through the existing `metadata.requires` (Decision 4). It replaces shared bundled trees. |
| `herdr: { min, tested }` and `cloned_from` | Adopted in the sidecar (Decisions 8 and 9). |
| List of names Herdr ships | Adopted as `host.shipped` in the profile (Decision 3). |
| Theme and icons kinds | Adopted (Decisions 3 and 5). |
| `permissions` manifest checked in CI | Not adopted. The host cannot enforce it; the analysis capability summary covers disclosure (Decision 7). |
| Separate installer with its own `installed.json` | Not adopted. It duplicates ownership state agent-marketplace already keeps. |

## Open items

- **TUI and web presentation**, including the approval prompt and drift
  views (Decision 11 is only a sketch).
- **Import and author-tool syntax:** package boundaries and mapping
  location are proposed in Decision 13; declared dependencies, manual
  setup metadata, and CLI spelling for dry-run/live verification remain
  open (Decision 12).
- **Analysis policy for host packages:** proposed in Decision 13.
- **Unmet package requirements.** `requires` is advisory unless
  `requires.expand` is on, but a layout without a required mod does not
  work. Undecided: place it and report it as not ready, or refuse it.
- **Organization preapproval and retirement UX:** the policy syntax for
  allowed destination and executable bindings and the explicit user action
  for retiring an old installation remain open. Decision 14 settles the
  canonical-root ownership binding and safety boundary, not that UX.
- **Cross-filesystem place publication.** Refusing publication when the
  agent-marketplace state directory and target root are on different
  filesystems blocks those installs; whether to offer a supported alternative
  remains open.
- **Adopting existing hand installs.** Before release, document either a
  safe first-release refusal and remediation path or design a separate,
  explicit opt-in adoption flow. Never adopt implicitly, even when bytes
  match. Pane-switcher and cos-queue collided with hand-installed files in
  the seeded scratch home. A later adoption flow cannot treat byte equality
  or a path under a store as durable ownership proof; shipping adoption
  itself is not required if safe refusal is the chosen path.
- **Organization block on unverified host versions:** exact policy shape.
- **Profile format paper and executable tests** against real Herdr app
  layouts and omp before freezing v1; the Tern trial is only a scratch
  placement exercise and is not a v1 approval.
- **Blocker, application root overrides versus engine root policy.** Tern
  documents a `TERN_CONFIG_DIR` override and the trial routed one to an absolute
  scratch path; this conflicts with the engine guarantee that arbitrary
  absolute roots are refused. Decide how explicit overrides are constrained
  or approved, including `HERDR_APP_HOME`, before freezing v1.
- **Herdr app root on Windows:** not documented by the Herdr app; confirm
  before declaring it.
- **Appending to a layer that declares no target list.** Target lists
  replace across layers instead of merging, so writing
  `targets: [herdr-app]` into a user layer that had none would replace the
  organization's harness list for that user. Proposed: seed the new list
  with the currently effective one. The cost is that later organization
  additions no longer reach that user.

### Carried from the adversarial review, not yet resolved

- **Register store versus package generations.** Decision 14 chooses the
  existing immutable package-generation and ownership-reference seam for
  place delivery. Whether register-mode generations share that store remains
  open with `target-profile-register`; register delivery is not part of this
  decision.
- **Profile identity and retention:** proposed in Decision 13. Unregistering after a profile update or removal remains open under Decision 6 and `target-profile-register`.
- **Staging and publication.** The proposed staging, cross-filesystem
  refusal, destination containment and observation limits are settled in
  Decision 14; the acceptance witnesses there remain unimplemented.
- **Herdr replaces its shipped mods on every start.** `host.shipped`
  (Decision 3) refuses known shipped names at curation. A later Herdr
  release may still ship a name we already placed. The guide documents
  that an edited shipped file is moved to `<name>-edited` and replaced by
  Herdr's version; whether a placed package of the same name is treated
  that way is untested. Decision 14 defines how the engine reports and
  retains bytes it observes as edited, but it cannot promise which copy
  Herdr's own startup behavior will keep.
- **Discovery and profile selection:** proposed in Decision 13.
- **Required selections** of a host package whose target is not enabled
  or not available on the machine.
- **Windows** is outside the supported platforms in `CODING_STANDARDS.md`,
  while the profile schema and the tern roots include it.
- **Register details:** a package-supplied `{id}` with a leading `-`;
  `herdr plugin enable/disable` state; backup cannot restore host-side
  registration. Exact-path ownership checks and recovery behavior are
  specified in Decision 6.
- **Edited owned files and failed verification.** Decision 14 chooses
  keep-on-sync/upgrade, cede-on-deselection, and retain-on-failed-check;
  its per-transition witnesses remain unimplemented.

## Delivery plan (proposed OpenSpec changes, in order)

Before freezing the `target-profile-place` contract, close the executable
second-target trial, existing-install refusal or adoption path,
initial-check/readiness evidence contract, and the
analysis-policy/assessment-binding gates already called out below.
Decision 14 settles the publication/crash/symlink and root-identity
design; its acceptance witnesses remain part of those changes.
`generalize-target-kinds` can start independently once the
target-model home, glossary and kind matrix decisions are settled; it need
not wait for the full place feature.

1. `generalize-target-kinds`: per-target accepted kinds and delivery
   modes; invariants and catalogs keyed by accepted kinds. No user-visible
   behavior change for existing harnesses.
2. `targets-alias`: `targets:` read as the same list as `harnesses:`
   under `apiVersion: agent-marketplace/v4`, the static target-ID rule,
   editors that keep the authored spelling, TUI first-launch support.
3. `target-profile-place`: profile format v1, the virtual
   `target-profile` resource kind from built-in and external sources,
   approval bound to profile revision and resolved destinations, explicit
   retirement of previous-root installations, place delivery with
   curation sidecars, declarative source import and self-contained native
   payloads, exact support-file permissions and single-owner
   destinations. Include conflict planning that preserves installed
   projections, sidecar `metadata.requires` with host kinds as
   prerequisite kinds, `verify`, platform/detect/version conditions, the
   sidecar program list and the shared author dry-run.
   Define and implement the exact profile binding and delivery-mapping
   identity in host-package assessments across Marketplace ingestion,
   publication and online/cached client delivery. No aggregate merging.
4. `herdr-app-target`: built-in Herdr app profile with mod, layout and
   theme kinds and the shipped-name list, curated registry layout,
   Marketplace ingestion/analysis of mods and complete app bundles,
   package host versions and `cloned_from`, manual setup reporting,
   explicit author live verification and TUI QA journeys. Exercise the
   four source shapes in Decision 4 through one profile. Needs the
   analysis policy and enforced package/profile assessment binding first.
5. `target-profile-aggregates`: owned-entry aggregates declared by
   profiles, including `on_edit`; `keys.json` and `programs.json` for the
   Herdr app, the `icons` kind and fragment-only packages.
6. `target-profile-register`: register delivery, store layout, live
   inventory and ownership checks before mutation, recorded inventory
   drift, binary binding, journaled previous/desired registration paths
   and exact-state recovery.

Changes 3 and 4 depend on the prerequisite stack (#626, #643, #661, #671,
#672) being merged. Selecting a Marketplace layout together with its
mods in one step also needs the later release that records Marketplace
requirement edges; until then curated collections cover that case.

Later changes cover Herdr plugins and omp (register mode plus binary
analysis policy), tern, the presentation work, writing `targets:` by
default and deprecating `harnesses:`.

The relevant OpenSpec changes must prove these acceptance cases:

- A package assessed under profile P refuses delivery under approved
  profile Q, online and offline, even when package bytes are unchanged.
- An unmanaged same-ID registration survives installation unchanged.
  A failed upgrade that leaves the old generation registered remains
  pending; recovery does not report it as the new generation.
- Destination conflict outcomes do not depend on package iteration order.
  An installed package keeps its projection when another package claims
  one of its paths; two new conflicting packages both install nothing.
  A package claiming a shipped host name is refused at ingestion. An
  unowned file at the exact desired path is not implicitly adopted even
  when its bytes match.
- A layout that requires a mod package warns with the mod's install
  command when expansion is off, and selects the mod from a Git or
  directory catalog when `requires.expand` is on. A host kind in
  `metadata.requires` is not reported as `catalog.requires.invalid`.
- A program listed in the sidecar is never a `requires` item, and a
  missing one does not fail `sync --strict`.
- Changing a root override writes nothing at either location until
  approved. Approval of the new destination does not prune the old one.
- Native mod directories and complete personal app bundles use one
  `herdr-app` profile through explicit import mappings. A complete bundle
  places its layout, mods and permitted root support files together;
  undeclared destinations and unowned collisions refuse safely.
- A mapped input or declared packaged dependency outside the selected
  source root refuses import. Making a larger repository available does
  not permit runtime dependence on an uninstalled checkout sibling.
  Cover the links-helper failure with a fixture that exercises the
  installed Python import, rather than relying on static detection.
- A queue-layout fixture with unresolved placeholders is not treated as
  working merely because placement or a host load check succeeds.
  Known incomplete queue and links packages are refused at curation, not
  accepted on the strength of that check. No engine path substitutes
  values or runs an upstream installer.
- A theme with no verify rule is reported as unverified, not passed.
- Pane-switcher activation, layout selection and deferred key bindings
  remain visible as manual setup. Placement and verification cannot
  clear those requirements implicitly.
- Author dry-run and actual ingestion/placement agree on package
  identity, payload, mapping and refusal diagnostics. Dry-run changes
  no target/config/lock/approval/ownership state and starts no host or
  package process; absent destination checks are reported as untested.
- Explicit live verification requires approval and reports the exact
  revisions and checks exercised without implicitly installing or
  activating packages.
- A second place target uses declarative profile and import data for its
  roots, package topology and metadata exclusions, with no target-ID-specific
  branch.
- Decision 14's per-transition witnesses cover publication, conflicts,
  root/symlink binding, recovery, edits and failed verification. The Herdr
  spike did not prove those behaviors.

Automated fixtures reproduce these source shapes and failures locally;
they do not require access to the example repositories, credentials or
machine-specific paths.
