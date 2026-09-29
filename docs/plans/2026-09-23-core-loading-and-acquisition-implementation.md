# Implementation plan: separate configuration intent from acquisition

Date: 2026-09-23

Status: **Implemented across the stack.** WP0 is PR #317 (`cgxc.8.1`); WP1 is PR #318 (`cgxc.8.2`); WP2 is PR #321 (`cgxc.8.3`); WP3 is PR #334 (`cgxc.8.4`); WP4 is PR #346 (`cgxc.8.5`); PR #390 (`cgxc.7` shared inspection) sits in the stack between WP4 and WP5; WP5 is PR #397 (`cgxc.8.6` rebased). The `agent-marketplace-cgxc.8.7` layer closes the remaining acceptance gaps. Individual PR merge status is tracked by the reviews, not asserted here.

Baseline: the stack was rebased onto PR275 / `cgxc.5` (validated Workspace), which sits on current main. PR291 was closed unmerged and contributes nothing.

Governing rule for every work package: **the diff removes more decision points than it adds.** A package that lands a new seam and leaves the old path alive "for compatibility" is not done. Each package lists the code it must delete; reviewers check the deletions before the additions.

## 0. What the code does today, and what changes

One function fuses the two responsibilities the design separates:

| Today | Location | Problem |
|---|---|---|
| `loadV4RuntimeConfig` parses nothing itself, but is the only way to turn a parsed `V4Config` into a `Config`, and it reads and decodes the layer lock on the way | `internal/config/loader.go:516` | Every v4 consumer pays for acquisition, including logging, telemetry, GC, and editors |
| Four loaders call it: `loadConfigTree`, `loadConfigTreeWithReads`, `LoadRootConfigFromPath`, `LoadConfigFromPath` | `loader.go` | Four entry points, one shape, small drift between them |
| The online path calls `config.LoadEffective` up to three times per operation: once to discover `v4.marketplace-lock-required`, once with a fabricated lock to read policy, once with the real lock | `internal/pipeline/marketplace_runtime_lock.go:66-148` | Error-triggered retry to obtain policy that never depended on the lock |
| `marketplacelock.Provisional` fabricates `InstanceID: "bootstrap"`, zero digests, and registry IDs equal to slugs, solely so `toLegacyConfig` accepts the document | `internal/marketplacelock/lock.go:104-150` | Placeholder evidence flowing through code that treats evidence as trusted |
| `LoadEffectiveOptions.OrganizationRuntimeLock`, `organizationRuntimeLockOverlay`, `ConfigRead.RuntimeLock` exist only to smuggle that fabricated or transient lock into the loader | `effective_config.go:136-139, 300-358, 385-414` | Loader API carries acquisition state |
| GC loads a hydrated `EffectiveConfig` twice (once to fail fast, once inside the lease), builds synthetic `config.Config{Registries}` per layer, and passes `effective.Merged` to Marketplace GC | `cmd/agent-marketplace/gc.go:55-105` | GC's source of truth is a resolution model it does not need; the first load is what fails today |
| Lock reading for GC (`loadEffectiveLayerLocks`, `layerRequiresLock`, `readTrustedLayerLock`) lives in the Cobra package next to a second lock reader in `lock.go` | `gc.go:132-207`, `lock.go:~1400-1451` | Two readers, one contract |
| GC has three entry points that wrap each other: `GarbageCollect` -> `GarbageCollectWithLocks` -> `GarbageCollectWithReachabilityAndContext` | `internal/cache/cache.go:204-278` | Variadic `*config.Config` API kept alive by wrappers |
| `hasMarketplace` loop written twice | `loader.go:517-523`, `marketplacelock/lock.go:95-102` | Duplicate predicate |

After this slice:

```text
config.ReadIntent(ctx, opts)                       -> Intent          parse, validate, includes, authority, read set. No lock I/O.
pipeline.ReadDeployedLocks(ctx, intent)            -> Evidence        ReadLayerLock/decodeLayerLock per layer. No network.
config.Bind(intent, evidence)                      -> EffectiveConfig existing toLegacyConfig + MergeLayers. Pure.
pipeline.LoadEffective(ctx, mode, opts, policy)    = Bind(ReadIntent, ReadDeployedLocks, inline organization acquisition)
                                                     ConfigLoadOnline | ConfigLoadStrict | ConfigLoadCached; no retry, no provisional lock
pipeline.GarbageCollect(ctx, dryRun)               reachability from Intent + Evidence + local metadata, under lease
```

Five named functions. No strategy interface, no options struct with booleans, no session object. Transient organization acquisition is inlined inside `loadEffective` (one resolve, read-assertion revalidation, bind) rather than a separate per-call `AcquireOrganization`; the `config.LoadEffective` facade was deleted once its callers migrated.

## 1. Types

All in `internal/config` unless noted. Keep them small; add a field only when a consumer in this slice reads it.

```go
// Intent is what every applicable layer declared, before any acquisition.
type Intent struct {
    Layers   [5]LayerIntent // indexed by Source (SourceUnset slot unused); zero-valued when absent
    Errors   []error        // parse, validation, authority, include failures only
    Warnings []error
    Diagnostics ManagedInstallationDiagnostics
}

type LayerIntent struct {
    Source Source
    Path   string
    Reads  []ConfigRead        // config files only; never a lock
    legacy *Config             // v2/v3 tree, already merged across includes
    v4     *V4Config           // v4 tree, already merged across includes
}

// Settings is the lock-free auxiliary policy of one layer. toLegacyConfig
// already computes exactly these fields at v4.go:1736-1744; extract that
// block into (LayerIntent).Settings() and have toLegacyConfig call it.
type Settings struct {
    Harnesses           []string
    AllowedRegistries   *[]string
    AllowedMarketplaces *[]string
    Backup              *BackupConfig
    Logging             *LoggingConfig
    Telemetry           *TelemetryConfig
    Materialization     *MaterializationConfig
}

// SourceDecl is one declared acquisition source, schema-neutral.
type SourceDecl struct {
    Kind         SourceKind // Git, Marketplace, Directory
    CanonicalURL string     // Git/Marketplace; empty for Directory
}

func (l LayerIntent) Present() bool
func (l LayerIntent) Settings() Settings
func (l LayerIntent) Sources() []SourceDecl
func (l LayerIntent) NeedsLock() bool       // any remote source; replaces layerRequiresLock + hasMarketplace + HasMarketplaceSource
func (l LayerIntent) NeedsMarketplaceLock() bool
func (i Intent) TargetHarnesses() ([]string, Source) // the repo>user>org rule now in LoadEffective:204-213
```

`legacy`/`v4` stay private. No consumer outside `internal/config` branches on schema version. If a consumer needs something not exposed, add a method; do not export the trees.

```go
// Evidence is acquisition input keyed by layer. nil entry = no lock for that layer.
type Evidence struct {
    Locks [5]*lockfile.Document   // indexed by Source, same as Intent.Layers
    Reads []ConfigRead            // lock reads, AcquisitionLock: true
}
```

`EffectiveConfig` is unchanged in shape so existing consumers compile. `LayerResult` keeps `Config`, `Local`, `Errors`, `Reads`. `ConfigRead.RuntimeLock` is deleted.

In `internal/marketplace`:

```go
// GCRoots is what keeps Marketplace artifacts reachable. Built by pipeline, interpreted here.
type GCRoots struct {
    ExactPins        []config.MarketplaceEntry     // legacy v2/v3 config pins (existing path)
    LockedResources  []lockfile.MarketplaceLock    // v2 lock records; ArchiveDigest is already present
    UnlockedEndpoints []string                     // canonical URLs; retained via local metadata walk
}
```

In `internal/cache`, `GCReachability` gains `Sources []string` (Git canonical URLs) and drops `Configs []*config.Config`.

## 2. Work packages

Sequential. Each is one reviewable PR. Each ends with the listed deletions applied, `go vet`, the focused package tests, and the isolated command tests for the touched commands.

### WP0. Red fixtures and baseline pin

No production code.

1. WP0 is pinned to current main `1d092d7d13a8a2340973f3e13d5980a137b7b4ee`; PR291 was closed unmerged and contributes nothing.
2. Add the isolated command test `TestGcCommandV4OrganizationMarketplaceWithoutLockCollects` in `cmd/agent-marketplace/gc_test.go`:
   - v4 organization config at the test organization path with one Marketplace source, no `lock.yaml`;
   - a Marketplace endpoint URL that resolves nowhere (`https://marketplace.invalid`) and an `http.Transport` guard or `GODEBUG`-free approach already used by the E2E network fixtures that fails the test on any dial;
   - populated `marketplace/metadata/<hash(endpoint)>/<registry>/<kind>/<name>/<rev>.json` with a valid `ArchiveDigest`, its `artifacts/<digest>` directory, and one unrelated `artifacts/<orphan>` directory with the existing removable shape;
   - `gc` (not dry-run) exits 0, output names the orphan, active artifact still exists, orphan is gone, organization directory mtime and listing unchanged.
3. Add `TestLoadUserLoggingConfigIgnoresUnreadableLock` in `internal/config`: user v4 config with a Marketplace source, `lock.yaml` replaced by a directory. Logging read succeeds.
4. Add the telemetry equivalent in `internal/telemetry`.

All three fail on the baseline. They stay red until WP2 and WP4 respectively; that is the point.

### WP1. Intent and Bind inside `internal/config`

Goal: `loadV4RuntimeConfig` disappears; every existing public loader becomes a two-line composition.

Add:
- `readLayerIntent(path, canonical string, data []byte, kind layerKind, overlays map[string][]byte) (LayerIntent, error)` — the body of today's `loadConfigTreeWithReads` minus the `loadV4RuntimeConfig` call and minus the lock read.
- `(LayerIntent).bind(doc *lockfile.Document) (*Config, error)` — `v4.toLegacyConfig(kind, doc)` or return `legacy`. This is where `v4.marketplace-lock-required` is still raised when `doc == nil` and the layer has Marketplace sources. The error moves; it does not get softer.
- `ReadIntent(ctx, opts) Intent` — today's `LoadEffective` lines 163-199 without merge/allowlist work.
- `Bind(intent Intent, ev Evidence) EffectiveConfig` — per-layer `bind`, then today's lines 201-280 unchanged.
- `readDeployedLock(path string, overlays) (*lockfile.Document, *ConfigRead, error)` — the lock read from `loadV4RuntimeConfig:528-565`, moved verbatim. Lives in `config` for now because `LoadEffective` needs it; WP3 moves it to pipeline once the facade stops needing it.
- `LoadEffective(ctx, opts)` = `Bind(intent, deployedLocks(intent, opts.FileOverlays))`.

Extract `Settings()` from `toLegacyConfig:1736-1744` and call it there. Extract `Sources()` from the `names` loop at `toLegacyConfig:1746-1766` and from legacy `Registries`/`Marketplaces`.

Delete in this PR:
- `loadV4RuntimeConfig`;
- `loadConfigTree`, `loadConfigTreeFromRootData`, `loadConfigTreeWithReads` — replaced by `readLayerIntent` + `bind`; `loadV3OrV2Config` collapses into its one caller `LoadGlobalConfig`;
- `hasMarketplace` loop in `loadV4RuntimeConfig` and `marketplacelock.HasMarketplaceSource` (WP3 deletes its last caller; add `V4Config.hasMarketplaceSource()` now and point both at it);
- `loadEffectiveOrganization` (one-line wrapper around `...WithRuntimeLock`) — merge;
- the three near-identical `loadEffectiveUser/Repo/Local` bodies collapse into one `loadLayerIntent(source Source, path string, kind layerKind, overlays)`; the `IsHomeDir` guard stays as a caller-side condition, not three copies.

Do not delete yet: `LoadEffectiveOptions.OrganizationRuntimeLock` and `organizationRuntimeLockOverlay`. WP3 owns them; keeping them one PR longer avoids touching pipeline here.

Tests: existing `internal/config` suite must pass unchanged except for tests that assert on the deleted private function names. Add `TestReadIntentPerformsNoLockIO` using the directory-as-lock fixture from WP0 item 3 and asserting `Intent.Errors` is empty while `Bind` returns `v4.marketplace-lock-required`. Benchmark `LoadEffective` before/after; expect equal or fewer allocations because the settings copy is no longer duplicated.

### WP2. Settings readers on Intent

- `LoadUserLoggingConfig()` = `ReadIntent` for the user layer only, return `Settings().Logging`. Keep the single-layer scope: call `readLayerIntent` for one path, not the four-layer reader.
- `internal/telemetry/resolve.go:106-116` currently loads two full `*Config` trees via `LoadOrganizationConfig` and `LoadConfigFromPath`. Change `ResolveTelemetryConfig(organization, user *amconfig.Config, ...)` to take `organization, user amconfig.Settings`. `telemetryFromConfig` and `resolveHarnesses` read only `Telemetry` and `Harnesses`, so this is a type change, not a behavior change.

Deletion audit as implemented on `agent-marketplace-cgxc.8.3`:
- `LoadOrganizationPolicyConfig` and `organizationPolicyWithoutLock` are deleted together with the now-unreferenced `V4Config.policyConfig`; telemetry no longer needs a runtime-policy loader that tolerates an absent lock, because `LoadOrganizationSettings` never reads the lock.
- `LoadOrganizationConfig` survives: `internal/tui/actions.go` still binds the full organization layer.
- `LoadConfigFromPath` survives: production editors, pipeline, and cmd callers load explicit paths with includes.
- `LoadRootConfigFromPath` survives: mutation code needs root-only ownership. It no longer carries its own v4/legacy parsing branches; it delegates to the shared `readRootLayerIntent`, which preserves root-only semantics (ParseV4Config for v4, `loadV3ConfigWithAPIVersion` with apiVersion optional only for local, logging validation, path-bearing errors) and then runs `li.deployedLock(nil)` plus `li.bind(doc)`.
- `LoadGlobalConfig` survives: deinit, TUI, and other production callers still use it. All four survivors reach the shared intent-plus-deployed-evidence binding path; none keeps a private parser.

WP0 fixtures 3 and 4 are enabled and green here: `TestLoadUserLoggingConfigIgnoresUnreadableLock` in `internal/config/include_test.go` and `TestLoadAndResolveSettingsIgnoresUnreadableLock` in the new `internal/telemetry/resolve_lock_test.go`.

### WP3. Acquisition without provisional locks

As implemented, `internal/pipeline/acquisition.go` owns the layered lock readers — `ReadLayerLock`, `decodeLayerLock`, and `readLayerLockWithOverlays` — generalized over the layer loop that lived in `cmd/agent-marketplace/gc.go:132-179`, and exposes:

```go
func ReadDeployedLocks(ctx context.Context, intent config.Intent) (config.Evidence, error)
```

`config.readDeployedLock` was not moved: it still serves the config package's single-layer readers (`intent.go:433`). Decision (a): keep the split between the single-file config decoder and the layered pipeline readers, and track unification as a follow-up decoder task rather than unifying in this slice.

Rules, all already present in `gc.go` and `lock.go`, now in one place:
- organization: `ValidateOrganizationTrustUnit(path, true, true)` before reading; absent is `nil` with no error; present must decode, scope-validate, and pass `unselectedLayerIsFresh`;
- other layers: absent is an error iff `NeedsLock()`;
- present locks always decode and scope-validate;
- the `lockLifecycleError` type and its `lock.invalid`/`lock.stale` codes move to pipeline unchanged so CLI output is identical.

As implemented, the runtime load is one entry point with an explicit mode (`internal/pipeline/marketplace_runtime_lock.go`):

```go
type ConfigLoadMode uint8
const (
    ConfigLoadOnline ConfigLoadMode = iota  // may acquire the absent organization lock transiently
    ConfigLoadStrict                         // on-disk evidence only; missing required lock is an error
    ConfigLoadCached                         // reuse verified transient cache; never contacts the Marketplace
)

func LoadEffective(ctx context.Context, mode ConfigLoadMode, opts config.LoadEffectiveOptions, policy resolver.MarketplaceRevocationPolicy) config.EffectiveConfig
```

`loadEffective` composes `readIntent` + `readDeployedLocks`, then — only when the mode is online, the organization layer `NeedsMarketplaceLock()`, and its lock is absent — performs transient acquisition inline (cache lookup, `marketplacelock.Resolve`, read-assertion revalidation — once) before `config.Bind`. There is no separate per-call `AcquireOrganization`.

`marketplacelock.Resolve` needs allowlist and target harnesses; both come from `intent` (`Settings().AllowedRegistries` merged with existing precedence, `intent.TargetHarnesses()`), so the provisional load that existed to obtain them is unnecessary. The transient cache keeps its read-assertion revalidation, keyed on the intent read set plus the lock-absence generation exactly as `transientMarketplaceReadAssertions` does now.

`GCReachabilityReader` callers in the lease path call `ReadIntent` + `ReadDeployedLocks` again under the lease; that is the existing generation-safety rule, now two calls instead of a full hydrate.

Delete in this PR:
- `marketplacelock.Provisional`, `OrganizationProvisional`, `HasRequiredError`, `MissingOrganizationLayer` (folded into `NeedsMarketplaceLock` + `ev.Locks[org] == nil`), `marketplaceSourceRequests` if `Provisional` was its last caller, `minimumMarketplaceResourceLockBytes` if only used there, `BenchmarkProvisionalManyMarketplaceSources`;
- `LoadEffectiveOptions.OrganizationRuntimeLock`, `organizationRuntimeLockOverlay`, `loadEffectiveOrganizationWithRuntimeLock`'s overlay branch, `ConfigRead.RuntimeLock`, the `RuntimeLock` tagging loop in `loadOrganizationConfig:387-398`;
- `loadEffectiveWithTransientOrganizationLock`'s three `LoadEffective` calls and the `transientOrganizationMarketplaceLockUse` enum if nothing reads it after the rewrite;
- `cmd/agent-marketplace/gc.go:132-207` (`loadEffectiveLayerLocks`, `readTrustedLayerLock`, `layerRequiresLock`) and the second reader in `lock.go` at the `readLayerLock`-style function around line 1400 — one reader survives, in pipeline;
- `layerRegistries` and `remoteEntries` in `cmd` if their last callers were the deleted GC code; `pipeline/update.go:412 layerRegistriesForUpdate` is the same function — dedupe to `Sources()`.

Expected size: `marketplace_runtime_lock.go` 251 -> roughly 120 lines; `marketplacelock/lock.go` loses ~90 lines; `effective_config.go` loses ~50.

Tests: existing transient-acquisition tests in `internal/pipeline` must pass with fixtures adjusted only where they constructed provisional locks directly. Add `TestLoadEffectiveOnlineReadsIntentOnce` using a counting overlay or `ReadIntent` spy to prove one parse per operation. Add `TestReadDeployedLocksAbsentOrganizationIsNil` and `TestReadDeployedLocksMalformedOrganizationFails`.

### WP4. GC on reachability

`pipeline.GarbageCollect(ctx, dryRun bool) (GCReport, error)`:

1. `recoverDependencyTransactions` (move from `cmd/lock.go:427`; it is not CLI logic).
2. Define `readReachability := func(ctx) (cache.GCReachability, marketplace.GCRoots, error)` that calls `ReadIntent` + `ReadDeployedLocks` and derives:
   - `Sources`: Git canonical URLs from every present layer's `Sources()`;
   - `Locks`: `ev.Locks` non-nil entries;
   - `GCRoots.ExactPins`: legacy Marketplaces from layers where `legacy != nil` (via an `Intent` method, not by exporting the tree);
   - `GCRoots.LockedResources`: `doc.Marketplaces` for each non-nil lock;
   - `GCRoots.UnlockedEndpoints`: Marketplace `CanonicalURL`s from layers where `NeedsMarketplaceLock() && ev.Locks[i] == nil`. By WP3 this is only ever the organization layer; do not special-case it.
3. Run Git preflight and Marketplace preflight, then both collections, preserving today's order at `gc.go:86-105`. Report only successful removals.

`cmd/agent-marketplace/gc.go` becomes the Cobra wiring plus the two `Fprintf` loops: about 40 lines.

`internal/cache`:
- `GCReachability{Locks, Sources}`; `activeRegistryHashes` and `activeExactGitSources` take `[]string` instead of `...*config.Config`;
- collapse `GarbageCollect`, `GarbageCollectWithLocks`, `garbageCollect`, `garbageCollectWithContext` into `GarbageCollect(ctx, dryRun, reader)`; the variadic `*config.Config` form is deleted and its tests rewritten to build `GCReachability` directly;
- remove the `if ctx == nil { ctx = context.Background() }` guards; every caller passes a context.

`internal/marketplace`:
- `GarbageCollect(dryRun, roots GCRoots)`;
- `referencedArchiveDigests` keeps its pin path for `ExactPins`; adds `LockedResources` by taking `ArchiveDigest` directly from mirror-acquisition records (no metadata read needed; validate digest format); adds `UnlockedEndpoints` by walking `metadata/<HashURL(endpoint)>/` with `filepath.WalkDir`, bounded by the existing `validateCacheRegularFile` and decoding each `*.json` as `store.AssessedResourceRevision`, collecting valid `ArchiveDigest`s. Malformed metadata under an active endpoint fails closed exactly as a missing pin does today; a missing endpoint directory is a valid empty root.
- Directory depth is fixed (`registry/kind/name/rev.json`); reject anything deeper or shallower rather than walking arbitrarily.

Delete in this PR:
- `gc.go:55-105` loading and orchestration;
- `cache.GarbageCollect` variadic, `GarbageCollectWithLocks`, `garbageCollect`, `garbageCollectWithContext`, `hasEffectiveConfig`;
- `marketplace.GarbageCollect(dryRun, cfg *config.Config)` signature and its `cfg == nil` / `ValidateMarketplaceEntries` preamble (validation already happened in `ReadIntent`);
- `recoverDependencyTransactions` from `cmd`.

WP0 fixture 2 goes green here. Also add: `TestGCUnionRetainsDigestSharedAcrossLayers`, `TestGCEndpointRootRejectsMalformedMetadata`, `TestGCEndpointRootEmptyIsNoop`, and the dry-run-then-collect idempotence case. Benchmark GC on a populated multi-endpoint cache before/after; the endpoint walk adds I/O, so report it rather than hide it.

### WP5. Sweep

- `rg` for `LoadEffective(` outside `config`/`pipeline`: each remaining caller either legitimately needs a bound runtime model or should be on `ReadIntent`. List them in the PR; migrate the obvious settings-only ones; leave resolution consumers for slice 2.
- Remove transitional facades whose callers are gone.
- `AGENTS.md` lockfile paragraph and `docs/PRECEDENCE.md`: one sentence each that reading configuration does not read locks and that GC requires no organization lock. `CONTEXT.md` terms are already in place.
- Update the design doc status to "implemented at <SHA>".

## 3. Style constraints for implementers

These are the concrete forms of "less code" for this slice. Reviewers should reject a PR that violates one without a stated reason.

- **One predicate, one place.** `NeedsLock`, `NeedsMarketplaceLock`, `Sources` replace every hand-written loop over `Registries`/`Sources` that asks "is anything remote / Marketplace here". Grep for `IsLocal()` and `V4SourceMarketplace` loops after each PR.
- **No wrappers of wrappers.** If a function's body is one call with reordered arguments, delete it and fix the callers.
- **No `ctx == nil` guards.** Callers own their contexts.
- **No boolean options.** `Bind` takes evidence, not `ignoreLocks`. `GarbageCollect` takes `dryRun` because it already does and it is a genuine mode; nothing else.
- **Errors carry the boundary once.** `fmt.Errorf("read lock %q: %w", ...)` at the read; not again at every layer up.
- **Log at operation boundaries.** `slog` start/complete/fail triplets stay in `pipeline.GarbageCollect` and `LoadEffectiveOnline`. Inner helpers do not log; today several do at every level.
- **Move, then delete, in the same PR.** No "deprecated" comments, no `_legacy` suffixes, no parallel path left for a later cleanup.
- **Tests build the real type.** GC tests construct `GCReachability`/`GCRoots` directly; they do not fabricate `*config.Config` to reach the collector.
- **Comments are not added** except where a moved comment already existed. Existing comments move with their code.

## 4. Acceptance

The slice is complete when all of the following hold on the integration branch:

- WP0 fixtures are green.
- `./agent-marketplace gc` on a machine with a v4 organization Marketplace config and no `/etc/agent-marketplace/lock.yaml` exits 0 and performs collection.
- `rg 'Provisional|OrganizationRuntimeLock|RuntimeLock\b|marketplace-lock-required' internal cmd` returns only `bind`, `toLegacyConfig`, and tests asserting the error.
- `rg 'LoadEffective\(' internal cmd --glob '!*_test.go'` shows fewer callers than baseline, and every remaining caller is a resolution consumer.
- Net line count across `internal/config/{loader,effective_config}.go`, `internal/pipeline/marketplace_runtime_lock.go`, `internal/marketplacelock/lock.go`, `cmd/agent-marketplace/gc.go`, `internal/cache/cache.go`, `internal/marketplace/cache.go` is negative, excluding the new endpoint walk (report that separately).
- Before/after benchmarks for `LoadEffective` and both GC collectors are attached to the final PR.
- Every acceptance row in section 7 of the design has a named test.

## 5. Sequencing and ownership

One integration owner for WP1-WP4; they are serial because each edits the seam the next consumes. WP0 can be written by anyone now. WP5 can be split by file once WP4 lands.

Beads: parent `agent-marketplace-cgxc.8` (under the `cgxc` architecture-audit epic) with children `cgxc.8.1` (WP0) through `cgxc.8.6` (WP5), plus `cgxc.8.7` for the final acceptance evidence, each blocked by its predecessor.

PR275 / `cgxc.5` (validated Workspace) **is** the base of this stack: the seam PRs were rebased onto it, so Workspace-aware callers flow through the shared pipeline load. `ReadIntent` keeps `FindRepoRoot`/`FindLocalConfigDir`; a follow-up passes `Workspace` roots into `ReadIntent` and deletes the two finders from it.

`cgxc.7` (shared inspection, PR390) landed in this stack between WP4 and WP5. `cgxc.4` (serialized mutations) remains a slice-3 consumer of `Intent`/`Bind` and is not touched here. The TUI custom-`Loaders` reload at `internal/pipeline/pipeline.go:402` and the `config.readDeployedLock` versus `pipeline.ReadLayerLock`/`decodeLayerLock` decoder split are deferred follow-ups, per design section 6.
