# Separate configuration intent from acquisition

Date: 2026-09-23

Status: **Implemented at `57087a51d4f565fcd64e4c7e57dda8b52e0a580b`.** The operator agreed the separation and the requirement that GC work normally without an organization lock. This document specifies the first implementation slice; it does not authorize dispatch or approve merging other PRs.

Baseline: `27675b3bc71ed336020ed7069125af93e055583c` on `rework-architecture`, matching remote `main` at investigation time. PR291 also has active work beyond its published head; its final revision remains an integration input.

Audience: maintainers and implementers of the CLI and TUI core. Purpose: architectural explanation and delivery contract, not a new user configuration reference.

## 1. Decision

A valid configuration is readable independently of resource acquisition. The configuration module owns authored intent and policy. Acquisition supplies exact evidence when an operation needs it. GC consumes local cache reachability rather than a resource-resolution model.

The organization acquisition lock is optional and normally absent. Its absence is not an error, an incomplete-GC condition, or a reason to contact a Marketplace. GC and normal consumer operations never create it. An administrator-supplied lock remains exact, trust-validated input. Explicit administrator lock-generation tooling is not changed by this slice.

This deepens existing modules instead of replacing the configuration schemas, resolver, compiler, dependency transactions, or ownership implementation. It follows [ADR 0008](../adr/0008-use-layer-owned-acquisition-lockfiles.md): configuration owns selection; acquisition locks own exact acquisition.

```text
configuration roots and includes
              |
              v
validated configuration intent + provenance/read set
              |
              +--> logging and telemetry settings
              |
              +--> local cache reachability --> GC
              |       ^
              |       | applicable deployed locks and local cache references
              |
              +--> operation-owned acquisition --> runtime binding
                                                     |
                                                     v
                                           existing resolve/materialize
```

## 2. Evidence and scope

At the baseline:

- `cmd/agent-marketplace/gc.go` calls `config.LoadEffective` before its own optional-organization-lock handling.
- `internal/config/loader.go` loads a v4 tree and immediately calls `loadV4RuntimeConfig`. Marketplace adaptation requires a v2 acquisition lock, even for callers that only need configuration fields.
- `LoadUserLoggingConfig` and `internal/telemetry/resolve.go` use this acquisition-dependent loading path.
- `internal/pipeline/marketplace_runtime_lock.go` recovers from a lock-required error by constructing a provisional lock, loading policy again, acquiring exact records, and loading again.
- `internal/marketplacelock/lock.go` constructs placeholder identities and zero digests solely to obtain a policy view. Those placeholders are not genuine acquisition evidence.
- Git GC already retains mutable cache entries and Git object stores by configured source without an organization lock. Exact snapshots use exact lock references.
- Marketplace GC currently accepts hydrated `config.Config` pins and follows cached revision metadata to mirrored archive digests. Its collector does not reclaim collection metadata or index-only namespaces.

The first slice includes the shared intent-loading seam, its use by acquisition preparation, lock-independent logging/telemetry reads, and GC. It does not complete all CLI/TUI consolidation. Endpoint search, shared inspection, complete mutation orchestration, and removal of TUI fallback paths remain explicit subsequent consumers/work packages.

Non-goals:

- v5, automatic schema migration, a new universal selection schema, or rewritten resolution semantics;
- plugin kinds, harness descriptors, package installation, or new harness behavior;
- broader hosted update/rebind, alias, or dynamic-selector support;
- a persistent replacement for the organization lock, a new cache database, or inventorying every repository;
- expanding which cache namespaces GC may delete;
- one global transaction across config, locks, and every harness output.

## 3. The configuration interface

| Existing module | Owned behavior after this slice |
|---|---|
| `internal/config` | Read and validate intent; expose settings/source views; perform side-effect-free runtime adaptation when supplied with appropriate acquisition evidence |
| `internal/pipeline` and `internal/marketplacelock` | Select operation-specific acquisition behavior, obtain/validate evidence, revalidate input generations, and coordinate GC |
| `internal/cache` and `internal/marketplace` cache code | Interpret their own local cache topology, derive retention/candidate sets, and perform guarded collection |
| `cmd/agent-marketplace` | Translate command arguments and render outcomes, without reconstructing intent or GC roots |

Binding performs no acquisition, file writes, or hidden reload. Filesystem/network work remains in the operation and cache implementations. Prefer concrete values and focused functions over a generic workflow runner, global session object, or configurable strategy framework.

### Configuration intent

Configuration intent retains what was declared and where: layer authority, source declarations, requests and exclusions, settings, and include provenance. It contains no invented instance IDs, registry IDs, commits, or digests.

The interface exposes three useful views from one read:

1. Per-layer settings, preserving authority and explicit empty/omitted values.
2. Source declarations and resource intent, retaining layer-local bindings and declaration paths.
3. Diagnostics and a read set over the exact bytes that produced those views.

Internally, reuse the parsed legacy `Config` and v4 `V4Config` trees. A layer wrapper can retain either representation privately; callers must not branch on schema versions or receive a partially hydrated `Config` that looks ready for resolution. Existing v4 ordered decisions and legacy selector behavior remain intact. There is no third general-purpose configuration serializer or selection model.

A single-layer reader supports logging's user-only scope and telemetry's organization/user scope. The layered reader composes the same implementation for organization, user, repository, and personal-local context. Logging must not start loading repository policy merely because a shared reader exists.

The shared implementation owns:

- bounded regular-file reads and existing permitted config-symlink behavior;
- strict schema decoding and layer-field validation;
- include traversal, declaration-relative paths, source bindings, and precedence;
- organization configuration authority checks, separate from acquisition-lock trust;
- the existing allowlist and target-setting rules;
- requested/canonical path identity and read-generation evidence.

Read-set digests must describe the bytes actually parsed, not a later reread that could describe another generation. Overlays retain the original declaring path and layer authority; they cannot make organization configuration writable.

### Acquisition and runtime binding

Acquisition consumes validated intent and effective policy. It reads applicable locks, acquires resources when the operation permits it, and produces evidence separately from intent. Runtime binding uses intent plus that evidence to construct the existing resolver-facing model.

These are separate stages, not a `skipValidation` or `ignoreLocks` flag. Configuration-only consumers have no reason to call runtime binding. Binding cannot fabricate pins, turn an invalid deployed lock into an unlocked source, or interpret unavailable acquisition as an empty selection.

Existing `LoadEffective` and `LoadEffectiveOnline` entry points may remain transitional facades, but must compose the same intent reader and binding implementation. They must not retain another parser, include walk, or policy merger. The online path decides whether transient organization acquisition is needed from intent and lock state, not by catching `v4.marketplace-lock-required` and retrying a different loader.

Invocation-local reuse of verified transient acquisition remains valid. Its lifetime, policy read assertions, and organization-lock absence assertion still apply. Revalidation compares current input generations; it does not require repeated semantic parsing when those generations are unchanged.

## 4. GC reachability without an organization lock

Cache reachability is a set of local retention roots and their local references. It is not a complete installed-resource catalog and cannot authorize materialization.

| Input | Retention role |
|---|---|
| Declared Git source | Existing source-level retention for mutable cache and Git object store |
| Applicable exact Git lock | Existing exact snapshot references by source and commit |
| Legacy exact Marketplace pin or applicable v4 Marketplace lock | Exact local resource/collection references using existing validation |
| Unlocked organization Marketplace source | Canonical endpoint retention root, traversed through existing local revision metadata to mirrored archive digests |
| Plain directory source | No acquisition-cache ownership; never traversed or deleted by GC |

Roots are unioned across the current context's applicable layers and includes. A digest retained by any root cannot be deleted because another layer stopped referencing it. A lock in one layer cannot narrow an unlocked source root in another layer. GC continues to have current-context scope; it does not inventory other repositories or claim to protect their caches.

### Unlocked Marketplace sources

The initial retention granularity is the configured endpoint. Existing metadata is stored beneath `marketplace/metadata/<endpoint-hash>/<registry-id>/<kind>/<name>/...`, and its revision records contain archive digests. That is enough to retain locally cached mirrored artifacts for an active endpoint without resolving mutable registry slugs or choosing an upstream revision.

An unlocked v4 locator alone does not prove a stable registry ID or current revision. The collector therefore retains the endpoint's locally referenced revisions rather than inventing that mapping. This is normal source-root retention, analogous to keeping a configured Git source's mutable cache; it is not a failed or skipped GC. Unreferenced artifacts outside all roots remain collectible. Narrower pruning within an active unlocked endpoint is deliberately not part of this slice.

The Marketplace cache module owns this traversal and cache-key interpretation. It uses canonical endpoint identity, bounded metadata decoding, validated digest formats and containment-safe paths. Arbitrary metadata values never become filesystem paths. Cached metadata here can justify retention only; it cannot bypass acquisition verification or revocation policy during installation.

A cold cache is a valid empty inventory. An absent organization lock requires no special warning and causes no fetch. Corrupt existing data on a traversal that is required to establish reachability is a different condition: existing fail-closed safety applies before deletion. The implementation must not equate a missing optional input with malformed present evidence.

Collections retain existing local member references within the rooted endpoint. This slice continues to preserve collection manifests, revision metadata, index-only occurrence caches, and protected derived-cache namespaces that current GC does not collect. It does not pretend that this limited collector already prunes every Marketplace cache form.

### Collection lifecycle and races

GC orchestration belongs below the Cobra command adapter. The adapter supplies context, dry-run intent, and output rendering; it does not load runtime configuration or decide source reachability.

Preserve transaction recovery and established lock ordering: exact-cache namespace lease before cache mutation exclusion. Reachability must be derived from configuration, applicable locks, and local metadata loaded or revalidated under the collection lease, not from an earlier unleased runtime view. Config/lock generation changes, including an organization lock appearing, invalidate the prepared view before deletion.

Both Git and Marketplace preflight must finish before either begins deleting. Reuse the existing lock primitives; avoid nested reacquisition of the same cache mutation lock. A successful preflight is not a promise of rollback if a later filesystem removal fails. Report only removals that succeeded.

`--dry-run` uses the same reachability rules and deletes nothing. Existing dependency-transaction recovery behavior remains explicit and unchanged; this flag is not redefined as the stricter `sync --check` contract. Neither recovery nor collection gains authority to write organization files.

## 5. Settings and resource operations

Logging reads validated user settings directly from configuration intent. Telemetry resolution reads its existing organization/user policy scope from intent. Existing precedence, consent, defaults, and error fallback remain unchanged. Actual logging and telemetry side effects are separate from configuration loading; this design forbids acquisition I/O in the reader, not ordinary authorized logging.

Resource operations still require appropriate verified evidence. Online unlocked organization acquisition remains transient. Offline/check/locked guarantees, consent revalidation, supported old lock/source-revision formats, and config-plus-lock transaction protections remain unchanged. Policy must be checked before network acquisition and checked again where existing commit semantics require it.

A document can therefore be valid for settings and source discovery while a requested runtime operation is unsupported or lacks acquisition evidence. Those errors belong to the attempted operation. They must not silently erase requests or report an installation as healthy.

The first slice does not make writable user/repository/local locks optional for operations that currently require them. It does not loosen validation of a supplied organization lock. The optional organization-lock contract is not a general ignore-lock-errors policy.

## 6. Integration and deletion obligations

| Existing work | Relationship |
|---|---|
| PR284, PR286, PR288, merged | Retain layer-aware validation, transient acquisition safeguards, and v4 editor regressions |
| PR291, published head `dcb2fbfc744f55d3a6e2b24802a961f0697ece24` | Baseline must include its final reviewed mixed-lock preservation and editor/parity work; active work is not frozen by this document |
| PR293, inspected head `cf0e0fb83a5bfb068242c89be24953a2f4db99e0` | Preserve doctor's exact-input and pre-mutation revalidation protections |
| PR275, inspected head `6f1adcedbe85e15ab82bff8ed957caebc647d303` | Reuse its validated Workspace rather than create another root authority; keep parser code independent of the pipeline package |
| PR289 and PR287 | Preserve settled organization-required policy and truthful operation output; externally owned is not synonymous with mandatory selection |
| PR160–162 materialization/diagnostics stack | Do not invent competing materialization-plan or diagnostic contracts; no blanket merge approval or dependency on unrelated changes |
| Plugin/harness-contract proposals | Separate effort; consume this loading seam rather than create a competing selection or acquisition interface |

The extraction must remove, in the migrated paths:

- lock hydration from the intent-reading stage;
- provisional lock identities/digests used only to recover policy;
- error-triggered parse/reload recovery used to obtain the same intent;
- logging and telemetry dependence on runtime pin hydration;
- GC's need for a hydrated `config.Config` as its source of truth;
- duplicated policy computation introduced by transitional facades.

Strict runtime compatibility entry points may survive. Production custom loaders, TUI mutation fallbacks, and the rest of command-owned lock orchestration are follow-up deletion obligations, not claimed completed here. Every migrated facade must delegate to the one implementation rather than preserve an independent path indefinitely.

## 7. Acceptance evidence

Expected results must be independently authored through the real consumer interface. The missing-organization-lock case is a required success case, not an expected refusal.

| Case | Required outcome |
|---|---|
| V4 organization Marketplace, no organization lock, remote unavailable, active and unrelated orphan artifacts cached | GC succeeds, retains artifacts referenced by the active endpoint, removes a genuine orphan, makes zero Git/Marketplace requests, and creates/changes no organization files |
| Same intent with empty cache | Normal empty GC result; no acquisition attempt or lock requirement |
| Unlocked source plus exact references in other layers | Union retention preserves shared digests; an exact layer cannot erase the unlocked source root |
| Present valid organization lock | Exact references honored without rewriting the lock |
| Present malformed, untrusted, wrong-scope, or stale applicable lock | Stable error and no cache deletion during failed preflight; never downgrade to unlocked |
| Missing required writable-layer lock | Existing refusal contract preserved; organization absence is not confused with this case |
| Mixed Git/Marketplace configuration and lock v2 | Both acquisition families survive loading and reconciliation; no v2-to-v1 loss of Marketplace records |
| V2/v3/v4 and mixed layers, includes, nested-local workspace | Same source binding, precedence, authority, paths, explicit-empty behavior, and materialization settings |
| Logging/telemetry settings with unavailable acquisition state | Same settings and policy as the acquisition-ready control; no lock hydration or acquisition network activity |
| Config or deployed lock changes while collection waits for its lease | Fresh reachability used or generation conflict returned before deleting newly referenced content |
| Malformed rooted metadata, symlink escape, oversized data, invalid digest | Bounded stable failure with protected/external bytes unchanged; no broad filesystem walk |
| Dry run followed by collection on unchanged inputs | Same candidate set; dry run deletes nothing; repeat collection is idempotent |

The GC fixture must prove both retention and deletion. A cache with no candidate orphan or a blanket no-op cannot satisfy it. Cold-cache tests are separate from corrupted-present-data tests.

Config/pipeline tests own pure intent and binding contracts. Isolated command tests own GC and startup reachability through production wiring. Existing CLI E2E fixtures cover mixed-layer and transaction behavior; later TUI consolidation retains a small real-terminal journey set rather than duplicating the entire matrix in terminal tests. All execution must isolate HOME, XDG roots, organization policy, and network fixtures per [coding standards](../../CODING_STANDARDS.md).

Changed loading and GC hot paths require before/after benchmarks. Measure both ordinary configuration and a populated multi-source cache; include local metadata I/O when claiming GC performance. No parsing or safety check is removed merely to improve a benchmark.

Investigation evidence on the baseline: existing focused isolated-command batches covering GC and transient organization acquisition/install/update passed. The exact v4 Marketplace GC regression was not present in those GC tests and was not newly implemented during this design work. Passing surrounding tests is not evidence that this design is implemented.

## 8. Delivery boundaries and Bead mapping

These are proposed work boundaries, not dispatcher assignments. Implementation should have one integration owner; policy/interface changes land serially rather than one independently designed loader per subcommand.

| Boundary | Inputs and output | Acceptance and task authority |
|---|---|---|
| Shared intent/acquisition seam | Final stabilization baseline; existing parsers, layer policy and read assertions → one intent reader and strict runtime-binding adapter | Settings and acquisition parity cases above; one bounded new child under existing `agent-marketplace-cgxc` is appropriate if no current child covers the seam. No new parallel architecture epic |
| Lock-independent GC | Intent seam, existing cache topology and lease primitives → local reachability and common preflight/collection orchestration | Non-vacuous no-org-lock GC regression and safety matrix; remains part of the same first-slice contract, not an independent ad-hoc GC patch |
| Shared inspection, later | Intent/acquisition seam and validated Workspace → one inspection result for status/check/list/TUI | Existing `agent-marketplace-cgxc.7`, already dependent on `cgxc.5` / PR275; preserve intentional discovery-policy differences |
| Complete shared mutations, later | Settled v4 editors, intent/acquisition seam and existing transactions → CLI/TUI operations with explicit committed/materialized outcomes | Existing `cgxc.4`, `0u73.1`, and remaining `j3z2`; reconcile PR291 coverage before assigning work and keep alias expansion separate |

The first slice can be reviewed as multiple commits, but cannot be declared delivered when only an unused abstraction exists. Its exit condition includes actual GC and settings consumers, removal of provisional-policy loading in migrated acquisition preparation, and the acceptance evidence above. It does not claim the entire core-lifecycle stabilization program is complete.

## 9. Alternatives rejected

- **Use the online loader everywhere:** makes local/configuration-only operations acquire resources and still conflates intent with availability.
- **Ignore lock errors or omit the organization layer:** loses source roots and policy and can authorize unsafe collection.
- **Require, auto-create, or replace the organization lock:** contradicts its optional externally managed role and the operator's explicit contract.
- **Treat normal organization-lock absence as partial or failed GC:** promotes an adapter limitation into product behavior; explicitly rejected by the operator.
- **Add a persistent local substitute lock:** introduces another authority and migration problem without being necessary for source-root retention.
- **Rewrite all configuration/resolution into a new model first:** expands risk and makes correctness dependent on a second broad cutover.
- **Add another facade without deletion:** leaves callers responsible for the same sequencing and fails the module deletion test.

## 10. Design provenance

The operator approved the corrected architecture in conversation after rejecting the missing-organization-lock GC dilemma. The original “partial cleanup or refusal” proposal is superseded by normal lock-independent GC.

Pinned Blackboard planning context (caller-authored rationale, not execution authority):

- Objective: `agent-marketplace/0b378e25-fc48-4664-a612-a64c6b739022`, revision 1.
- Operation contract question: `agent-marketplace/241fe7fa-a0ed-4d84-ae83-2fb0f2cdd178`, revision 1.
- Corrected loading seam: `agent-marketplace/459e82fd-0ef6-4d0a-80b6-1d80856f43ea`, revision 2.
- Operator GC constraint: `agent-marketplace/a4c8bf1a-450b-446c-963b-34482ccc616a`, revision 1.
- Later adoption questions: `agent-marketplace/557594ac-d17c-4a12-a7c9-68b59f26a821`, revision 1.

The written design still requires review before implementation planning and lifecycle assignment. Beads remains work authority; no task statuses, dependencies, or dispatcher lifecycles were changed to publish this document.
