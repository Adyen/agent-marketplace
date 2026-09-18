# Hosted Marketplace administration design

**Status:** Approved for implementation; implementation in progress. Managed backend runtime is available after explicit maintenance adoption and capability checks, with SQLite worker/HTTP integration and PostgreSQL store verification. Acquisition previews, complete diagnostics/impact views, remaining UI integration, and full browser acceptance remain unfinished; this is not production rollout approval.
**Date:** 2026-09-18.
**Baseline:** `13333633dab636ddffcf0e95ba63cce9bfb232f0`.
**Audience:** Marketplace maintainers, operators, and design reviewers.

## 1. Purpose and agreed direction

The admin interface lets an operator see what the hosted Marketplace is configured to do, understand what it is actually doing, and review changes before applying them. It extends the existing Nuxt application rather than introducing another frontend or embedding HTML in the Go binary.

The following product choices were agreed during brainstorming:

- Manage the hosted Marketplace, not developer CLI configuration layers.
- Apply registry and catalog-policy changes directly, without a Git PR or deployment.
- Reuse the deployment's existing SSO or identity-aware proxy.
- Provide Registries, Policies, Activity, and read-only Instance pages.
- Separate configuration acceptance from indexing success.
- Keep infrastructure and secret provisioning deployment-managed.

The user approved implementation of these technical contracts after reviewing this document. They describe the target behavior, not a claim that every acceptance criterion is implemented or verified.

## 2. Scope and alternatives

The first release supports registry creation and editing, license-policy editing and overrides, indexing status and retries, configuration history, and restoring previous settings as a new change. Registry-level pause, withdrawal, and retirement make management explicit without adding per-resource moderation.

It does not include resource-content authoring, individual-resource moderation controls, tenant isolation, custom roles, approval chains, secret uploads, infrastructure editing, or management of consumer `.agents` configurations. It does not remotely remove resources already installed on developers' machines.

Three approaches were considered:

| Approach | Benefit | Reason for choosing or rejecting |
|---|---|---|
| Git-backed config editor | Existing review and deployment history | Rejected for this workflow: the user wants direct application |
| Entire operator configuration in the database | One editable surface | Rejected: storage, identity trust, networking, and secrets should not be editable through the application they control |
| Database-backed catalog settings plus deployment-owned infrastructure | Live administration with a limited write surface | Recommended; each setting has exactly one authoritative owner |

A generic raw operator-YAML editor is not part of v1. Structured forms and a generated, read-only YAML representation cover the editable subset without exposing deployment settings or implying that comments from the original YAML round-trip through database revisions.

## 3. Existing implementation and required changes

These are observations about the baseline, not claims that the proposed functionality exists.

| Existing behavior | Design consequence |
|---|---|
| [Operator configuration](../../pkg/marketplace/config/config.go) is separate from consumer configuration | The admin module must not reuse `internal/config` layered edits |
| [`serve`](../../cmd/agent-marketplace/serve.go) loads YAML once and passes it to API and worker roles | Live configuration needs a shared database-backed read path |
| [`worker.Run`, `Refresh`, and `loadRegistryForJob`](../../pkg/marketplace/worker/worker.go) use startup configuration; refresh/status writes update registry records | Workers must stop treating startup YAML and whole-record upserts as authority for managed fields |
| [`MetadataStore`](../../pkg/marketplace/store/metadata.go) already provides transactions, registries, revocations, and durable jobs | Reuse SQLite/PostgreSQL and the existing queue, not a new database or broker |
| [`PublishRegistryRevision`](../../pkg/marketplace/store/atomic.go) uploads immutable blobs before a metadata transaction and checks the expected head commit | Extend publication fencing to configuration generations and live job leases |
| [`EnsureRevocations`](../../pkg/marketplace/worker/worker.go) reconciles the complete deployment-configured set; the API constructs its revocation evaluator at startup | Separate admin withdrawal ownership from deployment revocations and refresh availability at request time |
| The [Nuxt frontend decision](../adr/0003-separate-nuxt-marketplace-frontend.md) and [OpenAPI contract](../../api/openapi.yaml) define a separate, read-only browser | Add an authenticated admin interface and generated client without changing public routes into write routes |
| [`setImmutableCache`](../../pkg/marketplace/api/server.go) permits year-long caching of immutable payloads | Mutable availability requires revalidation; content-addressed bytes alone do not imply immutable permission to serve them |

The current security and operator guides describe an unauthenticated, deployment-configured application. They remain accurate until implementation lands; this draft does not silently replace them.

## 4. Screens and interaction model

Admin pages live beneath the configured Nuxt browser base: `/marketplace/admin` in the default deployment. They are private, not indexed by search engines, and never prerendered or stored in shared page caches. Existing catalog pages retain their purpose and routing.

### Registries

Registries is the default landing page. It favors actionable rows over charts.

```text
Administration / Registries                              [Add registry]

Search registries       Lifecycle: All       Health: All

Registry        Configured ref   Indexed commit   State       Last success
engineering     main             a31f92c          Active      8 minutes ago
security        v2.4             b029da1          Active      1 hour ago
experimental    main             —                Active      Never

A separate status column identifies indexing failures and policy findings.
```

Lifecycle, refresh outcome, and resource availability are separate concepts. A paused registry can have a successful last refresh; an active registry can contain blocked resources. One green/red badge must not conflate them.

A registry detail page has Resources, Configuration, Indexing history, and Findings tabs. Its persistent header shows immutable ID, slug, configured ref, last indexed commit, desired configuration revision, and the revision used for the last successful publication.

```text
engineering                         Active       Desired r18 / Published r17

[Resources] [Configuration] [Indexing history] [Findings]

Origin          git.example/engineering/resources
Ref             main
Source roots    .agents, .claude                Order is significant
Credentials     engineering-readonly           Reference only

Configuration accepted; indexing queued. Existing publication remains visible.

[Edit configuration] [Reindex] [More actions]
```

The editor covers slug, origin, ref, ordered roots and optional source harness, publisher metadata, default license, acquisition mode, credential/trust references, and supported license overrides. ID is immutable. Existing non-license overrides are preserved but read-only. Harness choices come from the backend's actual accepted operator schema, not a hard-coded copy of the consumer CLI's target list.

The add/edit interaction moves through **Edit → Validate → Review → Apply → Observe**. Drafts stay in page memory; navigating away prompts about unsaved edits. There is no autosave, shared draft workflow, or browser-local persistence of private configuration in v1.

Local validation checks syntax, names, paths, types, origin policy, and approved references without fetching a repository. An explicit asynchronous worker-backed preview can test acquisition and discover resources without publishing. Preview results identify the exact commit, configuration digest, policy revision, and time observed. Applying a mutable ref does not pin it to a previewed commit: the UI says that the ref can move before indexing. Applying an exact commit remains available through the existing ref field.

Review shows the configuration diff, known availability impact, and unknowns separately. A configuration without a successful network preview may still be applied after an explicit warning; static validation and security checks cannot be skipped. New registries remain empty until publication succeeds. Existing registries retain their previous publication unless a policy or withdrawal blocks it.

### Policies

The Policies page exposes the license allowlist and undeclared-license behavior, with effective per-registry overrides and inheritance explanations. Hidden-content analysis and resource/time limits remain deployment-managed in v1.

An empty global allowlist means enforcement is disabled, not that every license is prohibited. That choice receives explicit warning and confirmation. A missing registry override means inherit. At the baseline, an empty registry allowlist override does not disable enforcement; v1 rejects that ambiguous input rather than silently changing its meaning. A nonempty override may narrow or relax the inherited list, consistent with existing ingestion behavior.

Impact previews use stored license metadata and identify the catalog version they inspected. They show affected current resources and retained exact revisions, with pagination and honest unknown counts; new Git content requires acquisition. The interface never labels a partial scan as an exhaustive impact report.

### Activity

Activity combines two linked but distinct histories:

- Configuration and operational requests: actor, reason, timestamp, previous/new revision, diff, request ID, and resulting job IDs.
- Indexing attempts: queued/running/terminal state, attempt times, exact commit, configuration generation, sanitized diagnostics, and publication outcome.

A configuration change remains successful even if its indexing job later fails. Restoring settings opens a new reviewable draft against current settings; it does not rewrite history or silently restore old content. Registry restoration affects that registry only; global policy restoration affects only the selected policy fields. Ordinary settings restoration excludes lifecycle transitions and never removes accumulated aliases or historical origins; withdrawal and retirement use their explicit recovery actions. Missing credential references and current deployment restrictions are revalidated.

### Instance

Instance shows sanitized deployment-owned settings, application version, managed-config mode, current configuration revision, and queued/running work. It does not display DSNs, secret paths, environment values, or raw configuration dumps.

A worker heartbeat is not currently established by this design. The UI can report queue age and observed job activity, but must say “no recent worker activity” rather than claim a worker is offline without evidence. Private readiness and metrics listeners remain private.

### Explainability and accessibility

The main diagnostic journey is “Why isn't this resource available?” The backend provides structured reasons for discovery, publication, assessment, current license policy, withdrawals, and missing artifacts. The frontend renders those reasons; it does not parse log prose or reimplement policy.

Forms use existing Nuxt UI conventions, keyboard-accessible root reordering, field-associated errors, focus management, and non-color status labels. Dialogs identify the scope and consequences of withdrawal and retirement. The existing WCAG 2.2 AA and reduced-motion expectations apply.

## 5. Configuration ownership and adoption

The proposed deployment switch has two modes: legacy file-managed behavior and explicitly adopted database-managed behavior. Admin routes are disabled by default. Disabling the admin UI after adoption does not hand catalog authority back to YAML.

| Settings | Authority after adoption |
|---|---|
| Registry identities, metadata, origins, refs, ordered roots, acquisition mode, approved credential/trust reference IDs | Managed configuration in metadata database |
| Global license allowlist and undeclared-license behavior; corresponding registry overrides | Managed configuration in metadata database |
| Registry lifecycle and admin withdrawal declarations | Managed configuration in metadata database |
| Existing registry-specific non-license overrides | Preserved in the adopted database snapshot, read-only in v1; not reapplied from YAML |
| Instance identity/URLs, storage, listeners, refresh interval/concurrency, global non-license analysis and limits, catalog-projection exposure | Deployment configuration |
| SSO trust, admin group allowlist, permitted origins, credential/trust profile definitions | Deployment configuration |
| Existing deployment registry/coordinate/exact revocations | Deployment configuration, separate from admin withdrawals |

An explicit adoption operation validates the existing operator configuration and imports its catalog-managed values in one transaction. It preserves existing immutable registry IDs, aliases, historical origins, catalog content, and non-license overrides. References are converted to approved opaque profile IDs; unresolved mappings block adoption rather than importing arbitrary server paths. Adoption records its actor, source fingerprint, baseline revision, and database mode marker.

All API and worker processes must run a managed-mode-capable release before adoption. Initial activation is a controlled maintenance operation with old processes stopped, not a mixed-version rolling migration: old binaries do not know how to respect the new marker. After adoption, startup refuses a conflicting file-managed mode. Startup may report legacy YAML drift but never reapplies it. Returning to file authority requires a separately reviewed export/cutover, not a checkbox in the admin console.

The managed marker, revisions, audit records, jobs, and catalog belong to the metadata backup unit. Losing managed configuration while retaining an adopted database must fail closed, not rebootstrap from stale YAML. Database/blob backup and restore remain coordinated as described in the [backup guide](../marketplace-backup-restore.md).

## 6. Domain state and module responsibilities

Use one administrative module with a small interface for reading a configuration view, previewing a proposed change, applying it conditionally, requesting indexing, and reading activity. It owns validation, authorization context requirements, revision conflicts, audit attribution, and durable work intent. HTTP handlers and any adoption tool call this interface rather than writing store rows independently.

The store adapters implement its transactional persistence for both SQLite and PostgreSQL. This is a real seam because both adapters must satisfy the same observable contract. The frontend receives explicit admin DTOs, not serialized operator config structs.

The conceptual durable records are:

- **Managed configuration revision:** immutable normalized snapshot and parent revision, with a monotonically increasing instance revision.
- **Registry generation:** the version of settings and lifecycle relevant to that registry's jobs. Unrelated registry edits do not obsolete its work.
- **Policy revision:** the license-policy generation used for decisions, with effective per-registry inputs.
- **Change record:** actor, reason, scoped diff, revision IDs, request ID, and idempotency identity.
- **Work intent and attempt:** target registry/configuration generation, job kind, lease/attempt identity, progress, and result.
- **Publication record:** exact commit, root selection, configuration generation, policy digest, manifest identity, and publication time.

Configuration and worker status need distinct write operations. A late failure or success must not write an old merged registry record over a new origin, lifecycle, or ref.

A publication record is not just a Git commit. Editing roots or policy and indexing the same commit can produce a different publication. The baseline occurrence model is keyed around registry and commit; implementation must add a publication identity/current-publication pointer that distinguishes these cases while preserving immutable historical occurrences and public resource coordinates. Unchanged occurrences can be reused. Old manifests must never be rewritten to fit a new configuration.

## 7. Atomic edits, concurrency, and audit

A configuration read returns an ETag for the managed revision. Every write requires that version; the initial implementation deliberately uses instance-wide optimistic concurrency rather than unsafe automatic field merges. On a stale edit, the UI shows the latest diff and requires a new review.

An accepted change commits all of the following together:

- Normalized configuration revision and updated registry generations.
- Attributable audit record with previous/new state and a bounded reason.
- Availability state changes and catalog-version advancement where applicable.
- Durable indexing/preview work intent when needed.
- Idempotency result for the request.

If audit or work-intent persistence fails, the change fails; it must not appear saved without its required side effects. For a broad policy edit, one durable reconciliation intent can fan out bounded jobs after commit. A crash between acceptance and dispatch does not lose the work.

Mutations carry an idempotency key scoped to instance, actor, and operation. Repeating the same key and normalized body returns the original result; changing the body with that key is a conflict. Idempotency lookup precedes stale-version rejection for an identical accepted retry. Missing version, stale version, invalid fields, and conflicting idempotency each have distinct stable error codes.

Audit history is append-only through the application, not a claim of tamper-proof storage against a database administrator. V1 exposes no audit deletion or history-rewrite operation. Requests and diffs store profile IDs, never resolved secrets, raw identity headers, or unsanitized Git output. Reindex requests are audited even when they do not change a configuration revision. Indexing results link to the original request and preserve the initiating actor while identifying the worker separately.

## 8. Worker propagation and publication safety

```text
SSO/proxy → admin HTTP handler → administrative module
                                  │
                                  └─ transaction: config + audit + work intent
                                                        │
                                      existing durable queue
                                                        │
                        worker → acquisition → ingestion → fenced publication
                                                        │
                         public read → current availability + published content
```

Workers resolve database-managed settings when leasing work, rather than consulting the startup registry slice. Startup configuration continues to supply deployment-owned limits and profile resolution. The scheduler enumerates active managed registries. Manual reindex and the existing CLI reindex path use the same configuration authority; CLI use must not reconcile obsolete YAML over managed state.

A job carries registry generation, policy inputs, request identity, and operation kind. Scheduled idempotency keys include the applicable generation. A pre-acquisition check avoids knowingly obsolete work. Publication must recheck generation, permitted lifecycle, current policy inputs, expected head/publication identity, and unexpired lease ownership inside the same metadata transaction that makes content visible. A check before that transaction is insufficient.

If settings change during acquisition, the old result becomes superseded rather than overwriting the new desired state. A lease-losing worker cannot publish or report itself as the winning attempt. Superseded jobs do not retry forever; the durable intent for current settings supplies replacement work when appropriate. Blob uploads may leave unreferenced immutable blobs after a failed fence, matching the existing upload-before-metadata model, but those blobs must not become publicly reachable.

Workers update last-attempt and last-success fields separately. A failure preserves the last successful publication and records that desired settings remain unapplied. Availability restrictions still take effect independently; last-known-good content cannot override a withdrawal or a stricter live license rule.

No Redis, push channel, or file watcher is required. The UI polls bounded, paginated job views while an operation is pending. Network preview uses a separate durable job kind and temporary checkout; it runs the same acquisition and ingestion validation but cannot advance registry heads, publish catalog metadata, or return credentials. A preview of a not-yet-created registry carries an immutable validated proposal and base configuration revision rather than requiring a live registry row. Its result is bound to that proposal digest; changing the draft or relevant policy invalidates the preview. Apply revalidates the submitted proposal and current restrictions independently of preview success.

## 9. Registry lifecycle and identity

The proposed lifecycle is explicit and non-destructive:

| Action/result | Refresh work | Discovery and downloads | Recovery |
|---|---|---|---|
| Active | Scheduled and manual indexing allowed | Current publication, subject to policy/revocations | Normal operation |
| Pause refresh | New publication work rejected; old work fenced | Existing publication remains available and is labeled paused/stale | Resume activates and enqueues refresh |
| Withdraw | Refresh paused; old work fenced | Registry content denied on new authoritative reads; diagnostic identity remains | Restore enters paused state, removes only admin withdrawal, and rechecks remaining restrictions |
| Retire | Allowed only after withdrawal; no refresh | Hidden from default admin list; withdrawal and identity/history remain | Restore configuration creates a new revision in withdrawn state; separate restore/resume actions follow |

There is no hard delete or blob purge action in v1. Retirement is not a disguised destructive delete. Registry IDs and prior slugs cannot be reassigned to unrelated content. Origin changes keep a lineage only when the admin explicitly confirms that this is a move of the same registry; a different registry receives a new ID. Renaming a slug retains the old slug as an alias.

Pause and withdraw are linearized at their configuration transaction: publication after that transaction must fail its generation/lifecycle check. Work already running may continue briefly for cancellation cleanup, but cannot publish. A response whose availability check completed before a withdrawal may already be in flight; the guarantee covers reads checking authority after commit, not recall of bytes already sent.

Withdrawal is a separate admin-owned declaration, not a row that deployment reconciliation is free to delete. Effective denial is the union of deployment revocations and admin withdrawals. Restoring an admin withdrawal cannot cancel deployment registry, coordinate, or exact-revision revocations. Source attribution is retained even when both authorities deny the same registry.

Public diagnostic pages can explain revoked availability without serving installation bytes. Existing projection anchors remain visible as revoked, including retired registries, so consumers can reconcile identity. Retirement must not use the ordinary unlisted filter to erase these anchors. Per-resource moderation remains outside this UI.

## 10. Live license policy and public serving

Applying a license policy takes effect for new authoritative availability reads at commit, rather than waiting for the next refresh. The same typed license evaluator serves preview, ingestion, and read-time decisions; the frontend never reconstructs decisions from findings text. Existing rejected assessments remain a restriction, not permission that the live evaluator may override.

This is an explicit extension to the baseline, where the current assessment supplies the policy decision. A restrictive license change can immediately block a previously allowed exact revision using its stored license metadata. A permissive change does not automatically make an old rejected assessment or absent archive installable; it queues indexing of the configured ref for active registries. Paused registries receive the live policy change but wait for resume before indexing; policy edits never resume or restore a registry implicitly. Resources become available only when the current assessment and required artifact permit it. Historical rejected revisions not encountered by that indexing remain blocked in v1; bulk historical reanalysis is not promised by the admin interface.

Live license checks use the immutable revision's recorded license, not a newly edited registry default retroactively substituted into old content. Unknown/legacy metadata follows the configured undeclared-license rule. Current policy reason and assessment reason are reported separately, with their revisions, so the UI can explain a policy relaxation awaiting indexing.

Each availability-sensitive request obtains a coherent current managed-settings view from the database. V1 does not rely on a time-based process cache for enforcement. Database failure fails closed for affected reads rather than serving with a stale allow decision. Deployment restrictions remain effective alongside managed settings.

The shared availability logic must cover resource latest/exact reads, install metadata, artifacts, content, inspection bodies, registry/collection manifests, and catalog projection. Assessment metadata may remain inspectable as diagnostics but cannot be an alternate content-download path. Digest-only routes retain the existing conservative rule: any restricted owning revision restricts shared bytes. A withdrawal in one registry can therefore affect a digest shared with another; the confirmation preview must disclose this where ownership is known.

Managed availability changes advance catalog version/ETags. Projection retains blocked/revoked anchors, and conditional responses are evaluated only after current availability is known. Root changes update current publication membership without deleting historical exact identities; the discovery and projection model must not keep resources current solely because they existed under an older root selection.

For managed-mode content whose serving permission can change, responses require private revalidation rather than `public, max-age=31536000, immutable`. The proxy must not serve cached content without the authority check, including HEAD/conditional/range requests. Activation includes review and invalidation of controllable pre-existing shared caches. Browser caches and already downloaded artifacts cannot be recalled; the UI states this limit. Direct object-store downloads or long-lived signed URLs cannot bypass this policy.

## 11. Authentication, authorization, and request safety

The selected identity model is trusted-proxy SSO, not built-in accounts. The concrete provider/header mapping is deployment configuration, not an assumption about the user's current gateway.

The proposed contract carries a stable subject, an issuer/provider namespace, optional display name, and explicit group identifiers. Authorization requires membership in a deployment-configured admin group allowlist. Missing identity is unauthenticated; authenticated users without that group are forbidden. All admin reads, previews, and writes enforce this in Go. Hiding navigation is only a usability measure.

Identity headers are accepted only from an explicitly trusted immediate peer/network path protected by deployment network policy. Forwarded client-IP headers do not establish trust. The gateway strips incoming identity headers, sets its authenticated values, and protects the entire admin prefix. Conflicting or malformed identity fields fail closed. No public caller or ordinary Nuxt SSR request may synthesize an admin identity. The Go service is not directly exposed around the gateway. Local integration tests use an isolated test proxy, not a production auth bypass flag.

V1 admin requests are same-origin browser requests through the gateway. Admin pages load private data client-side; public SSR's internal API URL is not repurposed to forward identity headers. Admin cross-origin access is disabled even if public catalog CORS is supported.

Mutations require JSON, strict request limits, a custom request header, and an exact configured Origin match. Missing or mismatched Origin is rejected for browser mutations, not guessed from an untrusted forwarded Host. Together with disabled cross-origin credential access and gateway cookie policy, this prevents ambient SSO cookies from authorizing cross-site form submissions. No GET endpoint performs mutation. SSO expiry produces an explicit reauthentication flow and never silently resubmits an unreviewed edit.

Admin responses use `Cache-Control: no-store`; Nuxt payload caches, shared caches, logs, and telemetry do not retain private response bodies. Request rates and expensive acquisition previews are bounded per actor and instance.

## 12. Origins, credentials, and trust profiles

An authenticated admin can still misuse a repository fetch as a network request or send credentials to an unintended host. Browser-editable origin fields therefore remain constrained by a deployment-owned protocol/host/port policy, restricted worker egress, redirect checks, and repository-root validation. Local paths and `file://` origins are not accepted from the admin interface. Isolated test fixtures may exercise local acquisition without weakening production policy.

Credentials and private-CA settings are selected by opaque profile ID from a deployment-provisioned catalog. A profile binds permitted origins and protocols to its secret/trust references. The browser cannot supply arbitrary file paths, environment-variable names, SSH options, or CA contents. Origin edits revalidate the binding; credentials must not follow a redirect to a different unauthorized destination. TLS validation is never disabled.

Every worker resolves the profile locally at execution time. Missing mounts, revoked profiles, or rotation failures produce sanitized job errors. A known profile ID does not prove that every worker can use it, so validation distinguishes static reference acceptance from an actual worker acquisition test. Removing a profile does not erase the registry's configuration or expose its old secret.

Existing non-license overrides and safety limits remain preserved. New arbitrary override keys cannot be smuggled through an admin DTO. A move between acquisition modes is reviewed as a distribution change; changing to index-only is not a purge of previously mirrored blobs, and neither mode bypasses withdrawal checks.

## 13. Interface shape and errors

Admin HTTP routes use a separate `/api/v1/admin` namespace, disabled unless the deployment has enabled trusted administration and adopted managed configuration. The [Go-owned OpenAPI document](../../api/openapi.yaml) remains the transport contract; TypeScript is generated through the existing web tooling. These are proposed route families, not existing endpoints:

| Family | Responsibility |
|---|---|
| Session/capabilities | Current sanitized actor and permission/capability view |
| Registries and policy | Read views and conditional scoped configuration changes |
| Previews | Start/read bounded validation work without publication |
| Registry actions | Reindex and explicit lifecycle transitions |
| Activity and jobs | Paginated changes, attempts, and sanitized diagnostics |
| Configuration history | Read revisions and prepare scoped restore drafts |
| Instance and profiles | Read-only sanitized deployment context and approved reference IDs |

Writes return the accepted configuration revision plus linked work IDs, not a boolean suggesting indexing has completed. Async operations return an accepted/pending response; synchronous validation failures include structured field errors. Responses distinguish 401/403, missing preconditions, stale revisions, idempotency conflicts, invalid proposals, rate limits, and unavailable persistence using stable error codes. Raw database/Git/credential errors never become admin DTOs.

No external Git process runs in an HTTP request handler. Long-running discovery and acquisition remain worker responsibilities. Static validation and policy evaluation are shared modules, not duplicate implementations in transport handlers or Vue components.

## 14. Observability and verification expectations

The audit log is the durable explanation of accepted actions; structured `slog` events and spans explain execution. Logs link request, change, job, registry, configuration generation, and publication result without secret values or identity headers. Metrics use bounded labels such as action/outcome; actor IDs, origins, registry names, and arbitrary error prose are not metric labels.

These are implementation acceptance tests, not tests executed for this document:

| Area | Required observable evidence |
|---|---|
| Configuration adoption | IDs/history preserved; concurrent adoption serialized; missing profile mappings rejected; restart never overwrites DB-managed values |
| SQLite/PostgreSQL parity | Config, audit, idempotency, and work intent commit or roll back together; one winner for competing edits |
| Worker races | Edit, pause, withdraw, lease expiry, and policy update during acquisition prevent stale publication and stale status writes |
| Same-commit changes | Different roots/policy at one commit produce valid new publication state without rewriting old occurrences/manifests |
| Queue recovery | Crash after apply but before dispatch retains work; duplicate requests do not duplicate accepted changes; superseded jobs terminate |
| Live policy | Tightening blocks immediately; relaxing does not fabricate an archive or override a rejected assessment; empty global allowlist is explicit |
| Lifecycle | Pause preserves downloads; withdraw blocks new reads; retirement keeps tombstones/projection identity; restore never lifts deployment revocations |
| Serving | Latest/exact, digest-only, collection/registry manifests, inspection, conditional/HEAD/range, shared digests, and projection use the same current restrictions |
| Proxy security | Forged headers and direct bypass fail; non-admin SSO is forbidden; mutations reject cross-site/missing Origin; admin reads are not shared-cacheable |
| Origin/secret safety | Unauthorized hosts/redirects, arbitrary paths/env names, credential/profile mismatches, and secret-bearing URLs are rejected with non-leaking errors |
| Browser journeys | Add → preview → apply → successful index; failed preview; failed index after save; edit conflict; pause/withdraw/restore; session expiry; keyboard-only use |
| Compatibility/performance | Public read-only behavior in file mode remains unchanged; subpath routing works; availability reads, policy previews, and registry list queries have bounded-query benchmarks |

Implementation should extend existing Go/store conformance and E2E coverage plus the web Vitest/Playwright suites, not invent a second test harness. The current `web/package.json` defines `check`, generated-client drift checking, and `test:e2e`; the root Makefile defines `marketplace-gate`. Tests must use isolated identities, local fixture repositories, and disposable databases rather than production SSO, secrets, or registries.

## 15. Design handoff and dependent slices

The design divides into dependent deliverables, not independently authorized work packages. Configuration and lifecycle contracts must settle before frontend and worker work can safely proceed independently.

| Slice/question | Inputs | Output | Acceptance |
|---|---|---|---|
| Configuration authority: who owns live state? | Operator schema, transactions, existing jobs | Adoption/revision/transaction and worker-fence contract | Restart, concurrency, crash, and stale-publication cases have one defined outcome |
| Lifecycle: what remains visible and installable? | Configuration contract, identities, revocations, digest routes | State transitions and request-time availability contract | Pause/withdraw/retire/restore remain distinct across every serving path |
| Interaction/security: who can review and apply? | Both contracts, trusted proxy, Nuxt/OpenAPI | Admin DTOs, page flows, auth/CSRF/audit and verification contract | Direct requests cannot bypass authorization; saved/indexed states remain distinguishable |

A practical implementation sequence would establish managed state and adoption first, then worker/public-serving safety, then the authenticated administrative module and generated contract, and finally the four screens and full proxy/browser journeys. Admin writes remain disabled until the safety contracts work end to end. Implementation is authorized, but runtime activation remains a separate safety gate.

Blackboard planning inputs are pinned below. They are caller-authored planning context, not independent verification or execution approval:

- Root: `(agent-marketplace, 9b20c31c-43fa-488c-b6fe-946deed1f39a, revision 1)`.
- Configuration: `(agent-marketplace, 52661a7f-a6fc-4574-96dd-f9355047c4d9, revision 1)`.
- Lifecycle: `(agent-marketplace, cfd9b807-65f6-4ad2-b179-8feac3dd7d4f, revision 1)`.
- Interaction/security: `(agent-marketplace, f43a4356-d007-4e8a-81d2-93236c68de80, revision 1)`.
- Baseline worker findings: `(agent-marketplace, 1512c325-8a0b-4bef-98ad-cecc69dcf264, revision 1)`.
- Baseline revocation/cache findings: `(agent-marketplace, a82febb7-2eea-45bd-9d6c-3463dd89be2f, revision 1)`.

## 16. Review focus and remaining deployment inputs

The approved design fixes four consequential choices rather than leaving lifecycle behavior implicit:

1. **Explicit database adoption:** catalog-managed fields move out of deployment YAML authority, and activation requires a controlled cutover.
2. **Non-destructive lifecycle:** pause retains serving, withdraw denies serving, retire keeps history, and restoration does not automatically resume refresh.
3. **Immediate restrictive license policy:** new reads honor current licensing, while permissive changes may need indexing and do not automatically rehabilitate rejected historical revisions.
4. **Constrained administration:** one SSO admin permission, approved origin-bound credential profiles, same-origin requests, and no arbitrary operator YAML editing.

Deployment integration still needs the actual identity claim/header mapping, trusted proxy peers, admin groups, public origin, permitted Git origins, and provisioned profile IDs. These are required deployment inputs, not guessed values. The design can be reviewed without them; production enablement cannot.

The working implementation includes managed configuration and adoption primitives, lifecycle operations, publication identities and fencing, durable refresh attempts, persisted blob ownership, live availability checks, trusted-proxy HTTP handlers, and an initial Nuxt interface. The runtime guard still rejects database-managed operation. Controlled maintenance adoption, acquisition previews, complete diagnostics and impact views, and integrated proxy/browser acceptance remain unfinished. Frontend client generation and verification are blocked by the existing lockfile's incompatibility with the active dependency-release-age policy; no policy exception is part of this design. This document is not an activation guide.
