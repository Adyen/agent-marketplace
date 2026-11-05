# Design

## Context

This change implements the first slice ("Observation state and store leases") of the approved change-aware Marketplace reindexing design, imported in full under [Imported approved design](#imported-approved-design). See proposal.md for motivation and the delta spec for the normative requirements. The metadata store already provides a transaction helper (`WithTx`), the `jobs` queue with leases and attempt counting, the `ErrNotFound`/`ErrConflict`/`ErrLeaseExpired` sentinels, and an ordered, append-only migration list per backend whose last entry is the content-digest index migration.

## Goals / Non-Goals

**Goals:**

- Durable observation state, work lease, host budget, and refresh coalescing in both metadata backends, proven by one shared conformance suite.

**Non-Goals:**

- Git ref probing, the conditional scheduler, backoff, stable phases, and start-rate budgets (slice 2).
- Runtime config reload, indexing-generation computation, and stale-generation publication checks (slice 3).
- Metrics, dashboards, benchmarks, and operator documentation (slice 4).
- Any change to the current worker, its unconditional refresh interval, or `serve` wiring.

## Decisions

### Separate tables, appended migration

Observation state lives in a new `registry_observations` table keyed by registry ID, and host admissions in a new `host_budget_leases` table, both added by one new migration appended after the existing last migration in each backend. Existing migrations are not edited or renumbered. The observation table has no foreign key to `registries`, so removing or unlisting a registry never cascades into scheduler state, and removing an observation never touches registries, heads, occurrences, revisions, or assessments. Alternative considered: columns on `registries`; rejected because probe health would then share the public record and its update paths.

### Store interface

The `MetadataStore` interface gains these operations; both backends implement them on top of the existing transaction helper:

- `CreateRegistryObservation(ctx, RegistryObservation) (RegistryObservation, error)` — inserts version 1; `ErrConflict` when one exists.
- `GetRegistryObservation(ctx, RegistryID) (RegistryObservation, error)` — `ErrNotFound` when absent.
- `UpdateRegistryObservation(ctx, expectedVersion uint64, RegistryObservation) (RegistryObservation, error)` — compare-and-swap on `Version`; writes scheduling, observed, desired, and successful fields and increments the version; `ErrConflict` on a stale version, `ErrNotFound` when absent. It never changes the lease or active refresh, which have their own operations.
- `DeleteRegistryObservation(ctx, RegistryID) error`.
- `ListDueRegistryObservations(ctx, now time.Time, limit int) ([]RegistryObservation, error)` — due when `min(NextProbeAt, NextReconcileAt) <= now` and the work lease is not held at `now`; ordered by that due time, then registry ID.
- `AcquireRegistryLease(ctx, RegistryID, owner string, ttl time.Duration) (RegistryLease, error)`, `RenewRegistryLease(ctx, RegistryID, RegistryLease, ttl) (RegistryLease, error)`, `ReleaseRegistryLease(ctx, RegistryID, RegistryLease) error` — `RegistryLease{Owner, Epoch, Until}`. Every acquisition increments `Epoch`; renew and release must match owner and epoch of an unexpired lease, otherwise `ErrLeaseExpired`. A held unexpired lease refuses other claimants with `ErrConflict`.
- `RequestRefresh(ctx, RegistryID, RefreshRequest) (RefreshDecision, error)` — in one transaction sets desired commit/generation/trigger and either reports the existing equivalent active refresh (`Coalesced: true`) or enqueues the supplied job and records it as the active refresh. Any failure, including enqueue failure, rolls back both.
- `AcquireHostBudget(ctx, host, owner string, acquisition bool, HostBudgetLimit, ttl) (HostBudgetLease, error)` and `ReleaseHostBudget(ctx, HostBudgetLease) error` — admission counts unexpired rows for the host across all owners, including rows of the requesting owner; over capacity returns `ErrConflict`. `HostBudgetLimit{Operations, Acquisitions}`.

Exact field names follow `RegistryObservation` in the imported design's Domain model; `Version` is the compare-and-swap token.

### Terminal failure clears the active refresh in the same transaction

`FailJob` already runs in a transaction. When the failure exhausts the job's attempts, the same transaction clears `active_refresh_job_id` on any observation that references that job. Alternative considered: a separate call from the job package after failure; rejected because a crash between the two writes would strand the active refresh and suppress future attempts.

### Conformance suite shape

One table-driven suite takes a backend constructor and runs against SQLite always and against PostgreSQL when `POSTGRES_DSN` is set, as two top-level tests. Expected values are literals in the table. Delivery evidence runs the PostgreSQL test with a real database and pipes `go test -json` through `tools/witness-check -require`, so a skipped PostgreSQL run fails the check while default local and CI runs without PostgreSQL stay green.

## Risks / Trade-offs

- [Clock skew between workers on SQLite] → Lease and budget expiry use the same clock source each backend already uses for job leases; multi-replica deployments use PostgreSQL.
- [Interface growth breaks test fakes] → Fakes embed `store.MetadataStore`, so new methods need no fake changes.
- [PostgreSQL coverage is not in default CI] → Delivery evidence requires the witnessed PostgreSQL run; adding a CI PostgreSQL service is follow-up work.

## Migration Plan

The appended migration creates only new tables and indexes, so it upgrades existing databases without touching existing rows. Rolling back the binary leaves the unused tables in place; the previous version ignores them because migrations record applied versions and never drop unknown tables.

## Imported approved design

The approved design follows verbatim, with headings demoted two levels and its related-documentation links written as repository paths.

### Change-aware Marketplace reindexing

**Status:** Approved design, ready for decomposition

**Date:** 2026-09-18

**Origin:** Approved architectural brainstorming on 2026-09-18

#### Promotion boundary

The user approved this design before this document was written. The document authorizes implementation planning and work-package creation. It does not authorize implementation by itself.

The first release is provider-neutral. Forge webhooks and provider-specific APIs are deferred, but the observation boundary deliberately leaves room for them to supply change signals later.

#### Objective

Make the Marketplace worker observe registered Git sources and reindex only when the indexed result may change. A valid worker-safe operator configuration change must be noticed without restarting the process and must refresh only the affected registries. The design targets normal source-change discovery within about one minute for deployments with 20–200 registries while protecting shared upstream Git hosts from synchronized or unbounded traffic.

The worker must preserve the last known-good index whenever observation, configuration reload, acquisition, or ingestion fails.

#### Background

The current worker enqueues every configured registry at startup and once per `refresh.interval`. Each refresh creates an isolated repository, fetches the configured ref, checks out its tree, ingests every configured root, and publishes the result. This is safe but expensive when a ref has not moved: the default 30-minute interval repeatedly performs acquisition and ingestion without first asking whether the source changed.

The process also loads operator configuration once. `serve` passes value copies to the worker and API, so adding a registry, changing a tracked ref, or changing ingestion policy has no effect until restart. Manual `serve reindex` exists, but it requires an operator to know that a source or policy changed.

Git remains the publishing source of truth. The Marketplace must therefore observe Git rather than introduce another publication authority.

#### Goals

- Resolve registered Git refs cheaply before acquiring repository content.
- Refresh when the resolved commit or worker-relevant semantic configuration changes.
- Detect valid operator config replacements promptly, including Kubernetes ConfigMap-style atomic replacements.
- Coalesce duplicate signals and prevent overlapping work for one registry.
- Bound aggregate and per-host observation traffic.
- Coordinate correctly across worker replicas and process restarts.
- Retain the last successful registry head during every failure mode.
- Preserve manual forced reindexing and periodic unconditional reconciliation.
- Expose enough telemetry to distinguish freshness, upstream throttling, and failures.

#### Non-goals

- GitHub, GitLab, or other forge webhook endpoints
- Provider-specific REST or GraphQL change detection
- Persistent local Git mirrors
- Multiple tracked refs per registry
- Hot-reloading listeners, storage backends, Marketplace identity, or API revocation evaluators
- Deleting immutable history or artifacts when a registry is removed
- Guaranteed one-minute freshness when a shared upstream host budget is saturated
- Automatically changing existing authored `refresh.interval` values

#### Core mental model

Change detection and indexing become separate operations:

```text
config content observer                 due-source scheduler
          |                                      |
          v                                      v
  validated semantic diff               lightweight Git probe
          |                                      |
          +------------------+-------------------+
                             |
                             v
                  desired registry state
                  (commit + config generation)
                             |
                 unchanged  |  changed / forced
                   stop      v
                       durable refresh job
                             |
                             v
                clone, ingest, and publish
```

A registry needs a refresh when its desired indexing identity differs from its last successful indexing identity:

```text
indexing identity = resolved source commit + indexing generation
```

The source commit captures repository changes. The indexing generation captures semantic configuration changes that can alter acquisition, ingestion, assessment, or publication even when the Git commit is unchanged.

Observation answers whether expensive work is needed; it never changes the visible registry head. Only successful atomic publication advances that head.

#### Alternatives considered

##### Continue unconditional periodic refresh and add config watching

This is the smallest change, but it leaves the dominant waste intact. Every interval still performs acquisition and ingestion for every unchanged registry. It does not satisfy the upstream-load objective.

##### Probe refs before conditional refresh

This is the selected approach. A remote ref query transfers substantially less data and performs no checkout or ingestion. It is provider-neutral, fits the current Git authority model, and gives future webhook handlers a common observation interface.

A probe still creates work for the upstream Git server. Rate budgets, stable staggering, backoff, and periodic rather than continuous observation remain necessary.

##### Maintain persistent local Git mirrors

Incremental mirrors could reduce repeated object transfer and support tree-level comparison. They also introduce durable untrusted Git state, disk quotas, corruption recovery, locking, garbage collection, and shared-storage behavior across replicas. The existing sparse, filtered ephemeral acquisition should first be measured after unnecessary acquisitions are removed. Mirrors remain a possible later optimization.

##### Use provider APIs

Forge APIs can provide conditional requests, delivery identifiers, or batched queries, but they add provider-specific authentication and rate-limit behavior. The first release stays on the common Git protocol. Provider adapters may later feed the same desired-state transition.

#### Domain model

##### Registry observation

`RegistryObservation` is durable scheduler state, separate from the public `Registry` record and immutable occurrences. It contains at least:

- registry ID;
- last observed commit and observation time;
- last probe outcome and redacted error summary;
- next normal probe time;
- next unconditional reconciliation time;
- consecutive probe failure count;
- work lease owner and expiry;
- desired commit and desired indexing generation;
- active refresh job ID, if any;
- last successfully indexed commit;
- last successfully indexed generation;
- last successful index time.

The public registry refresh status continues to describe expensive refresh outcomes. Probe health must not overwrite a successful refresh status or imply that an unchanged probe published anything.

A separate observation record avoids adding one durable job per minute. At 200 registries, append-only probe jobs would otherwise create about 288,000 rows per day.

##### Indexing generation

The indexing generation is a versioned digest over a canonical semantic projection. It includes only values capable of changing indexed output or whether indexing succeeds:

- resolved origin and ref;
- roots and source harnesses in authored order;
- resolved acquisition mode;
- registry license;
- credential and CA references, but never resolved secret bytes;
- registry policy overrides;
- applicable global policy and acquisition or ingestion limits;
- a code-owned generation-schema version.

The projection uses deterministic field ordering and explicit normalized values. It does not hash YAML bytes. Comments, whitespace, mapping order, and equivalent defaulted forms therefore produce the same generation.

Aliases, publisher display metadata, refresh cadence, concurrency, listener addresses, storage settings, and other presentation or process fields are excluded. A separate semantic classifier handles those values.

The code-owned generation-schema version changes only when the projection or indexing semantics require existing commits to be reconsidered. Periodic reconciliation remains a defense against an omitted version bump.

##### Desired and successful state

The observation record distinguishes:

- **observed state:** what the latest successful Git probe reported;
- **desired state:** the newest commit and generation that should be indexed;
- **successful state:** the commit and generation used by the latest successful refresh.

A desired state is satisfied only when both its commit and generation match successful state. A policy change on the same commit is therefore real work. A branch rewind or force-push to a different commit is also real work.

#### Git ref observation

##### Acquisition boundary

The acquisition package gains a bounded operation conceptually equivalent to:

```go
ResolveRemoteRef(ctx, origin, ref, credentials, timeout) (commit, error)
```

It reuses the existing Git URL and ref validation, hardened subprocess arguments, credential environment, custom CA handling, non-interactive behavior, timeout handling, output truncation, and redaction. The worker must not implement a second authentication or Git-execution path.

The operation reads advertised refs without creating a checkout or downloading the repository tree.

##### Ref semantics

The resolver preserves the currently accepted ref spellings while making ambiguity explicit:

- `refs/heads/<name>` resolves that exact branch.
- `refs/tags/<name>` resolves that exact tag.
- `refs/remotes/origin/<name>` and `origin/<name>` normalize consistently with acquisition to the corresponding remote branch.
- A short name considers the exact branch and tag candidates. If both exist, observation fails closed as ambiguous rather than guessing.
- A lightweight tag resolves directly to its commit.
- An annotated tag resolves to its peeled commit, not the tag object.
- A missing, malformed, multiply matched, or non-commit result fails without changing desired or visible state.

The implementation must test parsing against local bare repositories rather than infer expected output from the production parser.

##### Full commit refs

A full commit is immutable under the repository identity model but may not be advertised by `ls-remote`:

- Before the first successful index, a full commit proceeds directly to bounded acquisition.
- Once the same full commit and indexing generation have succeeded, recurring remote probes stop.
- A config-generation change, manual reindex, or unconditional reconciliation can still acquire that commit again.

##### Probe-to-refresh races

A branch may advance between observation and acquisition. The refresh records and publishes the commit actually fetched. Publishing a newer commit than the observation is valid if the registry configuration generation is still current.

The worker rechecks the durable desired generation before network work and immediately before publication. It proceeds only when its local immutable config snapshot can supply the exact desired generation; a replica that has not observed that config yet defers the job without consuming a refresh attempt. If config changed materially during acquisition, the result is discarded and current desired work is scheduled. A stale worker must never publish an old origin, root set, or policy after a newer generation becomes authoritative.

#### Scheduling and upstream protection

##### Normal cadence

`refresh.interval` becomes the lightweight source-observation cadence. Existing authored values and the existing 30-minute default remain unchanged for compatibility. An operator seeking the target freshness configures `1m`.

Successful normal probes use a stable phase derived from the registry ID. The phase spreads registries across each interval and remains stable across process restarts, avoiding a startup herd and random long-term drift.

New and never-successful registries become due immediately, subject to global and host budgets.

##### Periodic reconciliation

The refresh configuration gains an unconditional reconciliation interval with a 24-hour default. Reconciliation bypasses commit and generation equality but still uses the same rate budgets, registry lease, and active-job coalescing.

This catches worker upgrades, missed generation-version changes, and unforeseen probe discrepancies. Reconciliations are distributed across their interval rather than enqueued together.

##### Probe timeout

The refresh configuration gains a probe timeout with a 30-second default. It is distinct from clone duration because ref observation should fail quickly and must not consume an acquisition slot.

The intended configuration shape is:

```yaml
refresh:
  interval: 1m
  reconcile_interval: 24h
  probe_timeout: 30s
```

All durations must be positive. Defaults remain owned by the operator-config package.

##### Concurrency and host budgets

Observation applies, in order:

1. a global observation concurrency limit;
2. one registry work lease, shared by probe and refresh work;
3. a deployment-wide budget for the normalized source host;
4. the probe timeout.

The host budget governs every remote Git operation, including probes and acquisitions. Initially, a host permits at most two active operations and at most one acquisition, preserving one slot for lightweight observation while a long clone runs. A shared start-rate budget prevents repeated short commands from turning concurrency into an unbounded request rate. The existing global refresh concurrency remains the additional acquisition-wide bound.

The authoritative host budget is durable so additional worker replicas do not multiply upstream traffic; a process-local limiter may reduce database churn but cannot replace the durable boundary. Host identity is derived from a validated, redacted origin. Credentials and URL user information never enter its key. Local `file` origins use a separate local budget and do not consume a remote-host allowance.

Host safety wins over freshness. If 200 registries share one slow host, due-probe delay rises rather than flooding the host. Telemetry makes that degradation visible.

Defaults remain in code initially. Additional operator knobs are added only when operational measurements establish a real need.

##### Backoff

A failed probe increments a registry-local failure count and schedules exponential backoff with full jitter. Backoff is capped at the slower reconciliation interval. It never delays unrelated registries or hosts.

A successful changed or unchanged observation resets the failure count and returns the registry to its stable normal phase.

##### One registry lease

Probe, scheduled refresh, reconciliation, and manual reindex all respect one durable registry work lease. This prevents a lightweight observation from racing expensive acquisition for the same registry and prevents multiple replicas from duplicating work.

Leases have bounded durations and become reclaimable after expiry. A worker that cannot acquire a registry lease does not wait while holding scarce host or refresh capacity.

#### Refresh coalescing

The logical refresh identity is:

```text
registry ID + desired commit + indexing generation
```

The observation record, rather than the current permanent job idempotency key alone, owns active-work coalescing:

- A transaction updates desired state and checks the active refresh job.
- If an active job already targets equivalent state, the new trigger is coalesced.
- If desired state advances while an older job runs, the older job may complete only if its generation remains current. A successor is scheduled when its result does not satisfy current desired state.
- A terminal failed job clears the active reference so a later observation can create a fresh bounded retry sequence.
- Trigger reasons are retained for telemetry: `source-change`, `config-change`, `reconciliation`, and `manual`.

This avoids a permanent identity key suppressing all future attempts after one job exhausts its retries.

Manual reindex always creates forced desired work, even when commit and generation equal successful state. It still coalesces with an equivalent active manual or reconciliation refresh and still respects registry and host safety boundaries.

#### Config observation and semantic reload

##### Ownership

The `serve` layer owns a versioned runtime config source because it owns the config path. The worker consumes immutable snapshots from that source rather than retaining startup value copies. Store and listener construction still use the startup configuration.

In a split deployment, the worker role observes worker-safe changes. The API role does not independently hot-apply those changes in this design.

##### Content polling

The config observer reads and hashes the configured file approximately once per second. It parses only when bytes change. Content polling is selected over notification-only watching because it behaves consistently on macOS, Linux, ordinary atomic file replacement, and Kubernetes ConfigMap symlink replacement.

Filesystem notifications may later reduce latency, but they are only a hint; periodic content observation remains the correctness path.

A missing, unreadable, partially written, malformed, or invalid replacement is rejected. The process retains the complete last known-good runtime configuration. It never interprets a transient missing file as an empty registry list.

##### Atomic application

A valid candidate is classified before it becomes current. Worker-safe metadata reconciliation and desired-generation updates occur transactionally where the metadata-store contract permits. If reconciliation fails, the runtime source retains the previous generation and retries the candidate later. Readers see complete immutable config snapshots, never fields mutated in place.

In multi-replica deployments, every worker replica must receive the same operator configuration. Durable generation checks prevent stale publication, but they are not a distributed configuration delivery system.

##### Semantic classification

| Change | Live action |
|---|---|
| Registry added | Upsert metadata, create observation state, and make it immediately due |
| Origin, ref, roots, source harness, acquisition, license, credential reference, CA reference, policy override, or applicable policy/limit changed | Advance that registry's indexing generation and schedule refresh |
| Global ingestion policy or limit changed | Advance every affected registry generation |
| Refresh cadence, probe timeout, or refresh concurrency changed | Reschedule only; do not refresh solely for this change |
| Alias, slug, publisher, or other mutable presentation metadata changed | Reconcile registry metadata without acquisition |
| Registry removed | Mark it unlisted and stop future observation; preserve history and identifiers |
| YAML comments, formatting, mapping order, or equivalent default spelling changed | No semantic action |
| Listener, operations listener, storage, or Marketplace identity changed | Report restart-required; do not apply live |
| Revocations changed | Report restart-required in the first release |

Slug and alias reconciliation continues to preserve lineage rules. Removing a registry writes an unlisted tombstone generation and clears desired work; pending or running work for an older generation cannot pass the publication precondition. Removal never deletes immutable occurrences, resource revisions, assessments, or blobs.

##### Revocation boundary

The API currently constructs a revocation evaluator from its startup config and compares that desired set with durable active revocations for readiness. Reloading only the worker's evaluator would make API filtering, ETags, and readiness disagree.

Revocation changes are therefore restart-required until API and worker share one atomic runtime evaluator. The config observer must not partially apply them or update durable revocations behind a startup-bound API.

##### Infrastructure drift

A valid candidate may combine worker-safe changes with restart-required changes. The worker-safe semantic diff applies, while the process reports that the remaining fields require restart. Startup-owned listener, instance, and store objects do not change.

The restart-required report is based on semantic values, not YAML bytes, and remains active until the running startup configuration matches the file again or the process restarts.

#### Publication and crash behavior

Publication keeps the existing blob-first, atomic metadata-head transition. The registry's expected head commit remains a compare-and-swap input. Generation currency becomes an additional publication precondition.

After successful publication, the worker records the actual fetched commit and applied generation as successful observation state. If the process crashes after publication but before this bookkeeping update, the next worker may perform one redundant refresh. This is safe and preferable to coupling blob publication and scheduler metadata into one cross-store transaction.

A failed refresh retains the prior head and records its failure. A failed probe does not alter refresh status. A malformed config does not alter either.

#### Failure behavior

| Failure | Result |
|---|---|
| Git probe timeout or remote failure | Keep last index; back off this registry |
| Missing or ambiguous ref | Keep last index; report probe failure |
| Host budget saturation | Delay due probe; expose due delay |
| Clone or ingestion failure | Keep prior head; use bounded refresh retry |
| Stale config generation before publication | Discard result; schedule current desired state |
| Invalid config replacement | Keep complete last-good config |
| Metadata reconciliation failure | Do not publish candidate runtime config |
| Worker crash with probe lease | Lease expires and becomes reclaimable |
| Worker crash after refresh publication | At most one redundant refresh |
| Registry removal | Unlist and stop scheduling; retain history |

Failures are contained to the narrowest registry, host, or config candidate possible. One unhealthy source does not stop the worker pool.

#### Observability

##### Structured logs

Meaningful decisions use structured logs with registry ID, trigger, durations, and bounded error information:

- probe changed, unchanged, or failed;
- refresh scheduled, coalesced, completed, or failed;
- config candidate applied or rejected;
- restart-required config drift detected or cleared;
- host budget delayed a due observation;
- stale generation prevented publication.

Origins pass through existing redaction. Tokens, credential values, headers, DSNs, and token-bearing URLs never appear in logs.

##### Metrics

The worker exposes:

- probe totals by outcome;
- probe duration;
- refresh totals by trigger and outcome;
- refresh duration;
- due-probe delay;
- host-budget delay;
- consecutive probe failures;
- time since last successful observation;
- time since last successful index;
- pending refresh queue age;
- config reload totals by outcome;
- restart-required config drift.

Registry IDs are bounded operator-configured labels where per-registry diagnosis is required. Raw origins and credential-derived host strings are not labels. Histograms use centrally owned buckets and avoid commit or generation labels.

##### Operator interpretation

An unchanged-probe counter increasing with stable last-index time is healthy. A growing due-probe delay indicates budget saturation rather than source failure. A recent successful probe with an old successful index indicates refresh failure or queued work. Config rejection and restart-required drift are distinct conditions.

#### Security considerations

- Observation reuses the hardened acquisition environment and URL/ref validation.
- No shell parsing or credential helper is introduced.
- Remote output is bounded, parsed strictly, and redacted before diagnostics.
- Probe and acquisition timeouts are independent and positive.
- Host keys exclude credentials and user information.
- Invalid or ambiguous ref responses never cause publication or unlisting.
- Config reload remains fail-closed and cannot swap storage or identity under a running process.
- Durable leases expire; no stale lockfile or process-local ownership determines authority.

#### Testing strategy

##### Pure unit tests

- Canonical indexing-generation projection and schema versioning
- Formatting-only and equivalent-default config changes
- Per-field semantic classification
- Global policy changes affecting the intended registry set
- Stable registry phase calculation
- Backoff with a fake clock and deterministic random source
- Host-budget admission and saturation
- Ref parsing for branches, lightweight tags, annotated tags, full commits, missing refs, and ambiguous short names
- Refresh desired-state coalescing and trigger classification

Expected values must be independently authored rather than computed with the production helper under test.

##### Store conformance tests

Both SQLite and PostgreSQL must prove:

- only one worker leases a registry at a time;
- expired work leases are reclaimable;
- due observation selection is deterministic;
- observation compare-and-swap rejects stale updates;
- host budgets are deployment-wide;
- desired-state and active-job updates are atomic;
- terminal job cleanup permits future attempts;
- registry removal preserves immutable records.

##### Worker integration tests

Local bare Git repositories exercise real Git without external network access:

- initial observation indexes once;
- repeated unchanged observations do not invoke acquisition;
- a pushed commit triggers exactly one refresh;
- a branch rewind triggers refresh;
- a policy change reindexes an unchanged commit;
- a full commit stops recurring probes after success;
- rapid source and config signals coalesce;
- stale-generation publication is rejected;
- probe failure retains the previous head;
- invalid config retains runtime state;
- removal unlists without deleting history;
- two workers do not duplicate probes or refreshes.

Acquisition and probe functions remain injectable at the worker boundary so tests can assert negative network side effects.

##### Serve-level tests

- Ordinary and atomic config replacement are observed.
- ConfigMap-style symlink-target replacement is observed by content polling.
- A malformed intermediate replacement never applies.
- Worker-safe and restart-required changes in one candidate are classified correctly.
- Cancellation stops config observation, probes, refresh workers, and timers.
- API startup-bound revocations are not partially changed.

##### Benchmarks

The scheduler scan, config semantic diff, and due-state selection receive benchmarks with a realistic 200-registry fixture. Benchmarks report allocations and keep setup outside the timed section. Network or disk inclusion is stated explicitly.

Before changing an existing measured hot path, implementation work captures and reports the required before/after benchmark evidence from `CODING_STANDARDS.md`.

#### Implementation boundaries

The design decomposes into four reviewable slices with explicit dependencies.

##### 1. Observation state and store leases

Produces the `RegistryObservation` domain type, metadata-store operations, SQLite/PostgreSQL migrations, store conformance tests, registry work lease, durable host budget, and active refresh coalescing contract.

This slice does not contact Git or change config reload behavior.

##### 2. Remote ref resolver and conditional scheduler

Depends on observation state. Produces the hardened remote-ref operation, ref semantics, stable due scheduling, backoff, conditional refresh decisions, manual/reconciliation integration, and source-observation telemetry.

This slice can initially consume startup configuration while proving that unchanged refs avoid acquisition.

##### 3. Runtime config source and semantic reconciliation

Depends on observation state and conditional scheduling. Produces content polling, immutable last-good snapshots, canonical indexing generations, semantic classification, restart-required reporting, registry add/change/remove reconciliation, and stale-generation publication checks.

It deliberately leaves API revocation hot reload out of scope.

##### 4. Operational completion

Depends on the first three slices. Produces serve-level replacement tests, full metrics, operator documentation, end-to-end multi-worker coverage, benchmarks, and any schema/reference updates required by the final implementation.

#### Acceptance criteria

The feature is complete when:

1. An unchanged branch observed repeatedly performs no clone, checkout, ingestion, blob publication, or head update.
2. A changed registered branch becomes desired within the configured observation cadence unless a visible host budget or backoff delays it.
3. Source change, same-commit indexing config change, manual reindex, and periodic reconciliation each trigger the correct bounded refresh behavior.
4. Formatting-only config edits trigger no registry work.
5. Invalid config replacements and all source failures preserve the last known-good index and runtime config.
6. Removed registries become unlisted without deleting immutable history.
7. Multiple workers do not multiply probes, host request rates, or equivalent refreshes.
8. Stale config generations cannot publish after a newer generation becomes desired.
9. SQLite and PostgreSQL pass the same observation and lease conformance suite.
10. Metrics distinguish unchanged health, source failure, refresh failure, config rejection, and host-budget delay.
11. Default tests require no external Git host, optional executable, or developer home state.

#### Consequences

The selected design replaces frequent unconditional acquisition with frequent lightweight observation and infrequent conditional acquisition. This substantially lowers transferred data, temporary filesystem work, ingestion CPU, analyzer work, and blob/store churn for quiet registries.

It does not make observation free. A one-minute target across many repositories still asks upstream servers to resolve many refs. Deployment-wide host budgets make that cost explicit and choose upstream safety over a hard freshness guarantee.

Durable observation state and leases add metadata schema and store complexity. That complexity is justified because process-local timers cannot prevent duplicate upstream work across replicas or survive restart with accurate backoff and due state.

Semantic config generations make reindex decisions explainable and deterministic, but they create a versioned contract that maintainers must update when indexing semantics change. Periodic reconciliation limits the impact of omissions.

The event boundary allows future webhooks to advance desired commit state immediately. Webhooks will optimize latency; they will not replace periodic Git observation, which remains the correctness backstop for missed or unavailable events.

#### Related documentation

- Marketplace serve and discovery (`docs/plans/2026-08-18-marketplace-serve-design.md`, not on main)
- Marketplace indexes Git and mirrors canonical artifacts (`docs/adr/0001-marketplace-indexes-git-and-mirrors-canonical-artifacts.md`)
- Marketplace operator configuration (`site/docs/reference/marketplace-operator-configuration.md`)
- How to reindex and monitor a Marketplace (`site/docs/how-to/operator/reindex-and-monitor.md`)
- Coding standards (`CODING_STANDARDS.md`)
