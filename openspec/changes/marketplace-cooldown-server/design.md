## Context

See `proposal.md` for motivation and
[ADR 0011](../../../docs/adr/0011-cooldown-clocks-and-history-rewrite-refusal.md)
for the clock decisions. Relevant current behavior:

- `GET /api/v1/resources/{registry}/{kind}/{name}` returns the revision at the
  registry head (`headOccurrenceJoins` in
  `pkg/marketplace/store/sqlite/resources.go` and its PostgreSQL twin). There
  is no notion of age anywhere in resolution.
- `registry_occurrences.occurred_at` is assigned by the store on first
  publication of a registry occurrence and excluded from `Equal`, so a retried
  publication keeps the original time. `resource_occurrences` link every
  revision to the occurrences that contained it. The earliest `occurred_at`
  over those links is the revision's first publication.
- Availability is a read-time overlay (`evaluateAvailability` in
  `pkg/marketplace/api/availability.go`) with values `available`, `blocked`,
  `policy-blocked`, `operator-exposed`, `no-artifact`, and `revoked`. Artifact
  and content routes consult it before serving bytes.
- Operator policy lives under `policy.*` with per-registry `policy_overrides`;
  `rejected_resources: block|expose` is the closest existing shape and fails
  validation on unknown values.
- Clients before this change enforce only assessment decisions and
  revocation (`enforceMarketplaceUse` in `internal/marketplace/resolver.go`);
  an unknown availability value does not stop them. Any server-side hold must
  therefore be expressed by *what `latest` returns*, not only by metadata.

## Goals / Non-Goals

Goals:

- Make a configured period elapse between first publication and resolution as
  `latest`, for every client version.
- Expose first publication and hold state so current clients can apply a
  stricter consumer floor and offer initial-acquisition acknowledgement.
- Keep existing Marketplace installations resolvable by exact digest.
- Keep the change schema-free in the metadata store.

Non-Goals:

- Consumer-side behavior (`git-cooldown`, `marketplace-cooldown-client`).
- Database-managed per-registry cooldown and its admin UI (follow-up).
- Hiding held revisions from browsing or history; a hold is not a revocation.
- Any change to ingestion, analysis, or publication.

## Decisions

### D1: First publication is a projection, not a stored column

`first_published_at` is computed as `MIN(so.occurred_at)` over the
`resource_occurrences` → `registry_occurrences` join for the revision's
`(marketplace_id, registry_id, kind, name, content_digest)`. It is added to the
assessed-revision projection (`assessedResourceColumns`) for every read path
that returns a revision: latest, list, exact, by-archive-digest,
by-content-digest, search, and catalog projection.

Consequences that follow from keying on content digest, accepted in the
design session:

- A revert to previously published content inherits that content's first
  publication and is eligible immediately.
- Archive-digest repair, reassessment, registry origin moves, and
  publication retries do not move it.
- A newly approved registry, or a metadata store rebuilt from scratch, holds
  every revision for one period. A restore from backup preserves
  `occurred_at` and does not.

### D2: Operator configuration

```yaml
policy:
  cooldown:
    period: 24h          # Go duration; default 0 (off); negative is invalid
    held: expose         # expose (default) | block; any other value is invalid
registries:
  - id: internal-gated
    policy_overrides:
      cooldown:
        period: 0        # per-registry period only; may be lower or higher
```

- `period` accepts the same duration syntax as `refresh.interval`.
- Per-registry overrides carry only `period`; `held` is global. A malformed
  override is ignored with a logged warning and the global applies, matching
  the existing treatment of non-`rejected_resources` overrides. The `held`
  key follows the `rejected_resources` rule instead: an unrecognized value
  fails the whole configuration, so a typo can never expose.
- In database-management mode the global `policy.cooldown` is carried into
  `admin.DistributionPolicy` at adoption beside `RejectedResources`, and read
  from the managed snapshot afterwards. File-mode registries read
  `policy_overrides.cooldown.period`; managed registries have no per-registry
  override until the follow-up adds the typed field. The existing
  `NonLicenseOverrides` bag is not used to smuggle it in.

### D3: Latest-revision resolution

For a coordinate with effective period `P` (registry override, else global)
and server time `now`:

1. If `P == 0`, behavior is unchanged: return the registry-head revision.
2. If the head revision's first publication is at or before `now − P`, return
   it.
3. Otherwise return the revision of that coordinate with the most recent first
   publication at or before `now − P`. Revocation, assessment, and license
   outcomes are not consulted for this choice; they are overlaid on the result
   as today, so a client sees the same refusal it would see for a head
   revision in that state.
4. Otherwise respond `409 Conflict` with body
   `{"error": "...", "code": "marketplace.cooldown.held", "revision": "sha256:…", "first_published_at": "...", "cooldown_until": "..."}`
   where `revision` is the head revision. `409` is already part of the API's
   error vocabulary; `404` is reserved for coordinates that do not exist, and
   clients translate it into "remove the pin".

The store gains `GetLatestEligibleResourceRevision(ctx, coord, cutoff)` on
both drivers; the handler composes steps 2–4 from the head read and that
query. Step 3 deliberately prefers the head when eligible rather than the
globally newest eligible revision, so a revert to old content resolves to the
head.

### D4: Availability overlay and distribution under `held`

`evaluateAvailability` gains a cooldown input (effective period, `now`) and
sets `availability: cooling-down` with `cooldown_until` when the revision is
younger than the period and no stronger state applies. Precedence, highest
first: `revoked`, `no-artifact`, `policy-blocked`/`operator-exposed`,
`blocked`, `cooling-down`, `available`. A held revision that is also rejected
reports the rejection; a client that could not install it anyway should not be
told to wait.

- `held: expose`: artifact, archive, content, and inspection routes serve a
  held revision exactly as an available one. Only `latest` withholds it.
- `held: block`: artifact, archive, and content routes refuse a held revision
  with the same status and body shape used for `policy-blocked`; metadata,
  revision lists, history, and inspection remain readable so the hold is
  explainable.

Unlike effective revocation, nothing is reconciled or stored; the overlay is a
pure function of `occurred_at`, policy, and the clock, so changing the period
takes effect on the next request after a configuration reload.

### D5: Caching

The latest-revision route today has no immutable cache header; it must not
gain one, and any `Cache-Control: max-age` it emits must not exceed the time
until the next eligibility boundary for that coordinate. Digest-addressed
routes keep `immutable`. The `409` body is `Cache-Control: no-store`.

### D6: Search and catalog projection

Rows keep anchoring on the registry head. They gain `first_published_at` and
report `cooling-down` through the existing availability field, so Backstage
and the web UI can distinguish a held head from an available one. Search
ranking is unchanged.

### D7: Web UI

The revision list and exact revision pages render a "Held until <date>" badge
from `current_availability` when availability is `cooling-down`. The latest
page needs no badge: `latest` is the eligible revision by construction. The
badge copy avoids the words blocked, revoked, and rejected.

### D8: Codes and logs

- Configuration: existing `marketplace/config` validation errors, extended for
  `policy.cooldown.period` (non-duration or negative) and `policy.cooldown.held`
  (unknown value).
- API error code: `marketplace.cooldown.held` (D3 step 4).
- Worker/API log: one debug line per `409` with coordinate, head revision, and
  `cooldown_until`; no new metrics series.

## Spec delta outline

These become the ADDED/MODIFIED requirements of this change when the spec
deltas land with their annotated tests (`tasks.md` 0.1). Scenario titles are
listed so reviewers can judge the contract now.

`marketplace-api`

- Requirement: Resource revision responses carry first publication
  - Scenario: First publication is the earliest containing occurrence
  - Scenario: Republished identical content keeps its first publication
  - Scenario: Archive-digest repair keeps first publication
- Requirement: Latest-revision resolution honors the server cooldown
  - Scenario: Eligible head resolves as latest
  - Scenario: Held head resolves to the newest eligible revision
  - Scenario: Coordinate with no eligible revision answers 409 with cooldown evidence
  - Scenario: Zero period preserves head resolution
  - Scenario: Registry override lowers the period
- Requirement: Held revisions report cooling-down availability
  - Scenario: Held revision reports cooling-down and cooldown_until
  - Scenario: Revoked or rejected held revision reports the stronger state
- Requirement: Held distribution follows the held policy
  - Scenario: Expose serves a held artifact by exact digest
  - Scenario: Block refuses a held artifact and keeps metadata readable
- Requirement: Cooldown configuration is validated
  - Scenario: Unknown held value fails configuration
  - Scenario: Negative period fails configuration
  - Scenario: Malformed registry override is ignored with a warning
- Requirement: Latest-revision responses are not cached past eligibility
  - Scenario: Latest response carries no immutable cache header

`marketplace-web-ui`

- Requirement: Held revisions are badged
  - Scenario: Revision list shows the eligible date for a held revision

## Risks / Trade-offs

- **Old clients on a held coordinate see an error, not a hold.** Accepted:
  failing closed is the purpose. The `409` body is machine-readable so current
  clients explain it.
- **Query cost.** `MIN(occurred_at)` per revision adds one aggregate to
  projection reads. Both drivers already join occurrences; the catalog
  performance benchmarks in `marketplace-catalog-performance` bound the
  regression and must not move beyond their existing budgets.
- **Clock skew between API processes.** Eligibility is evaluated with the
  serving process's clock; two processes can disagree for the skew duration.
  Acceptable for periods measured in hours.
- **Newly approved registries are fully held.** Accepted in the design
  session; the per-registry override is the escape hatch and the operator
  guide says so.

## Migration Plan

1. Land store projections and the eligible-revision query with tests.
2. Land configuration, availability overlay, latest handler, distribution
   gating, OpenAPI, and the public-gate E2E scenario.
3. Land the web UI badge.
4. Update `site/docs/reference/marketplace-operator-configuration.md`,
   `docs/marketplace-operator.md` (newly approved registries, fresh
   databases), and `docs/marketplace-security.md`.
5. Archive the change in the same pull request.

No data migration. Rollback is a configuration change (`period: 0`).

## Open Questions

- Whether `409` or `423` better signals "exists, not yet eligible". `409` is
  chosen because it is already in the API's vocabulary; reviewers may prefer
  otherwise before the OpenAPI delta is frozen.
- Whether search should optionally exclude held heads for consumers that only
  want installable results (`?eligible=true`). Deferred until a consumer asks.
