# Design

## Context

See proposal.md for motivation. This is the third slice of the approved change-aware reindexing design imported in `openspec/changes/archive/2026-11-03-add-marketplace-registry-observation/design.md` (sections "Config observation and semantic reload" and "Publication and crash behavior"). Slice 2 (`marketplace-source-observation`) added the observer, conditional refresh requests through `RequestRefresh`, and a conservative `startup-v1:` generation computed from startup values. `serve` loads the operator configuration once and passes value copies to the worker and API. The metadata store offers nested `WithTx` transactions in both backends; SQL shared between backends uses `$n` placeholders, which SQLite accepts.

## Goals / Non-Goals

**Goals:** a serve-owned runtime configuration source with immutable snapshots, a canonical indexing generation, semantic classification with restart-required reporting, atomic registry reconciliation, and a stale-generation publication precondition, all in operator-config management mode.

**Non-Goals:** API revocation hot reload and any API-role reload; filesystem notifications or signals; resizing the refresh worker pool at runtime; metrics, benchmarks, ConfigMap symlink and multi-worker serve-level tests (slice 4); unlisting registries removed while the process was stopped; database management mode and `serve reindex`, which keep their startup configuration.

## Decisions

### Runtime source in the operator-config package, owned by `serve`

`config.ParseConfig(data []byte) (Config, error)` holds the decode, defaults, and validation that `LoadConfig` performs today; `LoadConfig` reads the file and calls it. A `config.RuntimeSource` is created by `config.LoadRuntimeSource(path)`, which reads the file once, so `serve` uses the source's startup configuration for stores, listener, API, and revocations. The source exposes `Startup() Config`, `Current() Snapshot` (`Snapshot{Version uint64; Config Config}`, held in an atomic pointer and never mutated), `RestartRequired() []string`, and `Run(ctx, interval, apply)`. `serve` creates the source; `worker.Run` receives it and starts `Run` with the worker's reconcile function after initializing observation state, so no candidate applies before initialization. The poll interval is the code-owned constant one second; tests pass a short interval.

Each poll reads the file and compares its SHA-256 with the digest of the last bytes read. Equal digests return immediately. A read or parse failure is rejected: logged as `config candidate rejected` with a bounded redacted reason once per distinct digest (a missing file uses a fixed marker digest), and the snapshot is unchanged. A valid candidate is classified; the effective candidate is the parsed candidate with every startup-owned field copied from the startup configuration, so startup-owned values never change at runtime. When the classification is empty, the bytes digest is recorded and nothing else happens. Otherwise `apply(ctx, current, effective, classification)` runs; on success the snapshot advances to the next version and the digest is recorded; on failure the failure is logged and the digest is not recorded, so the next poll retries the candidate.

Alternative considered: a watcher based on `fsnotify`; rejected by contract NG2 and because content polling behaves the same for in-place writes, renames, and symlink swaps.

### Canonical projection and generation

`config.IndexingGeneration(cfg Config, registry Registry) (string, error)` returns `semantic-v1:` plus the hex SHA-256 of the JSON encoding of an explicit projection struct with fixed field order: schema version, origin, ref, roots (path and source harness, authored order), resolved acquisition (registry value or `acquisition_default`), license, CA file, credential references (SSH key file; token file and token env name; booleans for whether an inline SSH key or token value is present), policy overrides (JSON object encoding sorts keys), and the defaulted global `Policy` including limits. Inline secret values and token contents are never part of it. Because the projection is built from the defaulted configuration, comments, formatting, mapping order, and explicit defaults produce the same generation. The worker's `startupGeneration` is deleted and every generation use calls this function with the current snapshot. The old `startup-v1:` successful generations differ, so each registry refreshes once after upgrade.

### Classification

`config.Classify(startup, running, candidate Config) Classification` returns: `Added`, `Removed`, `IndexingChanged`, and `MetadataChanged` registry ID lists (each in candidate or running order), `RefreshIntervalChanged bool`, and `RestartRequired []string`. `IndexingChanged` holds registries present in both whose generation differs. `MetadataChanged` holds registries whose slug, aliases, or publisher differ and whose generation is equal. `RestartRequired` names, in a fixed order, each of `listen`, `operations_listen`, `storage`, `instance`, `revocations`, `management`, `admin`, `catalog_projection`, `installation_metrics`, `refresh_concurrency` whose candidate value differs from the startup value (compared with `reflect.DeepEqual` on the defaulted values). It is compared with startup rather than the running snapshot so the report persists until the file matches startup again. The source logs `restart-required config drift detected` with the field list whenever the list changes to a non-empty value and `restart-required config drift cleared` when it becomes empty. `refresh_concurrency` is restart-required because the refresh worker pool is sized at startup; this is stricter than the imported design's reschedule-only row and still never refreshes.

A candidate whose only differences are restart-required fields has an empty worker-safe classification and applies no registry work, but its restart-required report is still updated.

### Atomic registry reconciliation in the store

`store.ApplyRegistryConfiguration(ctx, meta MetadataStore, changes []RegistryConfigurationChange) error` runs every change inside one `WithTx`:

```go
type RegistryConfigurationChange struct {
	Registry          Registry  // complete record to put; Listed=false unlists
	DesiredGeneration string    // set when non-empty
	ClearDesiredCommit bool     // removal clears desired work
	DueAt             time.Time // when non-zero, sets NextProbeAt (NextReconcileAt kept >= it)
}
```

For each change it puts the registry, creates the observation record when missing, and, when a desired field or `DueAt` is set, updates the observation with compare-and-swap on the version read inside the transaction. Any error rolls back every change. The worker builds the changes: added registries use `mergeStoreRegistry` and no desired fields (a new observation is due immediately); indexing changes use `mergeStoreRegistry`, the new generation as desired, and `DueAt` now; metadata-only changes use `mergeStoreRegistry` only; removals take the stored registry with `Listed=false`, desired generation `removed`, `ClearDesiredCommit`, and no `DueAt`. In database management mode the worker ignores runtime registry changes.

### Observer and refresh processing read the current snapshot

The observer reads `source.Current().Config` at each due registry instead of a stored startup copy, so `refresh.interval`, policy, and registries take effect from the next observation. A registry absent from the snapshot keeps the existing skip path (advance next probe, no network), which is how a removed registry stops being observed.

Refresh job processing takes the snapshot once per job and uses it for the whole job. Before leasing host budget or acquiring, it reads the observation: if the payload generation differs from the desired generation, the job is stale; if it equals the desired generation but the snapshot's generation for the registry differs (or the registry is absent from the snapshot), the job is released without consuming an attempt. A payload without a generation uses the snapshot's generation. `Refresh` publishes with `ExpectedGeneration` set to the payload generation.

A stale job is completed, not failed, so it consumes no attempt and does not touch refresh status; the worker logs `stale generation prevented publication` with registry, job, and both generations, and sets the observation's next probe to now so the current desired generation is requested.

### Publication precondition inside the publication transaction

`RegistryRevisionPublication` gains `ExpectedGeneration string`. On the non-managed publication path, inside the publication transaction and before any write, a shared query reads `desired_generation` from `registry_observations` for the registry; a missing record or a different value returns `store.ErrStaleGeneration`, which wraps `ErrConflict`. Publications without an expected generation (database management mode, existing callers) are unchanged. `Refresh` returns this error without recording a refresh failure. Checking inside the transaction closes the window between the pre-publication read and the head change.

### Telemetry

Structured `slog` records: `config candidate applied` (version, added, removed, indexing changed, metadata changed counts), `config candidate rejected` (bounded reason), `config candidate apply failed`, `restart-required config drift detected` and `cleared` (fields), and `stale generation prevented publication`. Reasons pass through the existing redaction and bounding.

## Risks / Trade-offs

- [Every registry refreshes once after upgrade because the generation scheme changed] → Accepted and named in the release note; bounded by host budgets and refresh concurrency.
- [A replica that has not yet read a new configuration defers jobs for that generation] → Intended; the job returns to the queue without consuming an attempt.
- [An omitted schema-version bump misses a semantics change] → Periodic reconciliation still refreshes every registry daily.
- [Registries removed while the process was stopped stay listed] → Unchanged from today; reconciliation compares candidates with the running configuration only.

## Migration Plan

No schema migration and no configuration field. Rollback to the previous binary ignores the new generation values and resumes startup-only configuration loading; recorded `semantic-v1:` generations then differ from `startup-v1:` and cause one refresh per registry.
