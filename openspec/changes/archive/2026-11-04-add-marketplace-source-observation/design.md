# Design

## Context

See proposal.md for motivation. This is the second slice of the approved change-aware reindexing design. It builds on `marketplace-registry-observation`: per-registry observation records with compare-and-swap updates, deterministic due selection, one registry work lease, deployment-wide host budgets (`AcquireHostBudget` with operation and acquisition limits), and `RequestRefresh`, which atomically sets desired commit and generation and coalesces onto an active refresh job.

Today `worker.Run` enqueues one refresh per registry at startup and on every `refresh.interval` tick, and `Refresh` clones and publishes unconditionally. `serve reindex` enqueues a one-attempt manual job and processes it in-process. Database management mode (`runManaged`) has its own scheduler and is out of scope.

## Goals / Non-Goals

**Goals:** observation-driven refresh in operator-config management mode using startup configuration; one Git execution path; deterministic scheduling that tests can assert with a fake clock.

**Non-Goals:** configuration reload and the canonical semantic indexing generation (slice 3); worker metrics, operator knobs for the new defaults, and benchmarks (slice 4); database management mode; a start-rate host budget; provider APIs, webhooks, and persistent mirrors.

## Decisions

### Remote ref resolution lives in the acquisition package

`acquire.ResolveRemoteRef(ctx, origin, ref, cred, timeout) (string, error)` validates origin and ref with the same validators as `clone`, builds the environment with `newWorkspace` (credential environment and CA copy), prefixes `hardenedGitArgs()`, and runs `git ls-remote -- <origin> <patterns>` through the same subprocess helper so failures are redacted and truncated identically. Standard output is read through a fixed byte bound; exceeding it is an error. A second Git runner or authentication path was rejected (contract NG4).

Ref semantics: a full lowercase commit returns immediately with no subprocess. `refs/heads/X` and `refs/tags/X` match exactly. `origin/X` and `refs/remotes/origin/X` strip their prefix and resolve like the short name `X`, consistent with `fetchArgs`. A short name `X` queries `refs/heads/X` and `refs/tags/X`; when both are present the tag wins, matching the ref `git fetch` selects during acquisition. A tag line with a peeled `^{}` entry resolves to the peeled commit. No match returns an error wrapping a not-found sentinel. Each output line must be `<40 or 64 lowercase hex>\t<ref>`; anything else fails.

`acquire.SourceHost(origin)` derives the host-budget key from a validated origin: lowercase host and port for URL and scp-like forms, with user information removed; `file://` and local paths map to one `local` key so local origins never consume a remote host's allowance.

### Generation is a conservative startup-config digest for this slice

The desired and successful generation recorded on the observation is `startup-v1:` plus the SHA-256 of the JSON encoding of the registry's operator configuration together with the resolved default acquisition mode and the policy section. It changes whenever any of those bytes-level values change across restarts, so it can only over-trigger refreshes, never miss one. Credential references (paths and environment variable names) are included; resolved secret values never are. Slice 3 replaces it with the canonical semantic projection.

### Observation loop

`Run` in operator-config mode upserts each configured registry record and creates its observation record if missing (zero next-probe time, so it is due immediately). An observer goroutine then polls `ListDueRegistryObservations` on a short tick and processes due registries with a small fixed concurrency. Per registry:

1. Acquire the registry work lease; on conflict skip.
2. Unless the ref is a full commit, acquire host budget admission (`acquisition=false`, limit two operations and one acquisition). On refusal log the host-budget delay (overdue duration and redacted host), release the registry lease, and leave the record due.
3. Resolve the ref with the code-owned probe timeout (30 seconds); release the host admission.
4. On failure, increment the failure count, record redacted probe health, and set the next probe time to the backoff. Desired state, the registry record, and the head are untouched.
5. On success, record observed commit and health, reset failures, set the next probe time to the next stable phase slot, and decide: observed commit or generation differs from successful state → `RequestRefresh` with trigger `source-change` (or `config-change` when only the generation differs); else reconciliation due → trigger `reconciliation` and advance the reconciliation time to its next phase slot; else nothing.
6. Release the registry lease.

Observation and refresh-request updates use the version read after the lease, retrying a compare-and-swap conflict by rereading.

Phase: `fnv64a(registryID) mod interval`; the next slot is the first instant strictly after now whose offset from the Unix epoch modulo the interval equals that phase. Reconciliation uses the same rule with the reconciliation interval (24 hours). Backoff for failure count n: `d = min(cap, interval * 2^(n-1))`, delay in `[d/2, d]` chosen from `fnv64a(registryID, n)`, capped at the reconciliation interval. Both are pure functions of their inputs and a clock injected into the observer so tests control time.

### Refresh jobs carry their desired identity

The refresh payload gains commit, generation, and trigger. Jobs are created only through `RequestRefresh` with unique idempotency keys; coalescing is owned by the observation record. Scheduled refreshes keep three attempts; manual ones keep one.

Processing a refresh job acquires the registry work lease and host admission with `acquisition=true`. If either is refused, the job is released back to pending without consuming an attempt and the worker pauses briefly. After a successful publication, the worker records the actually fetched commit, the payload's generation, and the index time as successful state. Clone, ingestion, and publication are otherwise unchanged.

### `serve reindex` requests a manual refresh

Reindex ensures the registry and observation records exist, calls `RequestRefresh` with trigger `manual` for the observation's current observed commit and generation, and then waits on and processes the returned job exactly as before. When an equivalent refresh is active, it waits for that job.

### Injection seams

`Deps` gains optional resolve and clone functions defaulting to `acquire.ResolveRemoteRef` and `acquire.Clone`, so integration tests can count probes and acquisitions against local fixture repositories without network access.

### Telemetry

Structured `slog` records: `source observation` (registry, outcome changed/unchanged/failed, commit, duration, bounded redacted error), `refresh requested` (registry, trigger, coalesced, job), and `source observation delayed by host budget` (registry, redacted host, overdue). Origins pass through `redact.URL`; errors come from the redacting Git runner.

## Risks / Trade-offs

- [The startup digest over-triggers a refresh after an irrelevant config edit and restart] → Acceptable: one redundant refresh per restart; slice 3 replaces the digest.
- [A short ref that names both a branch and a tag could resolve differently from acquisition] → Resolution prefers the tag, as `git fetch` does, so observation and acquisition agree on the commit.
- [Host-budget saturation delays freshness] → Intended: safety wins over freshness and the delay is logged.
- [A crash after publication before bookkeeping causes one redundant refresh] → Accepted by the approved design.

## Migration Plan

No schema or configuration change. On upgrade every registry has no observation record and is observed immediately; registries without successful state refresh once. Rollback to the previous binary resumes unconditional periodic refreshes; observation records are ignored by it.
