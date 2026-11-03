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
  `policy-blocked`, `operator-exposed`, `no-artifact`, and `revoked`. In
  database-management mode `managedResourceAllowed` also gates metadata reads
  on the availability value.
- Digest-addressed artifact and content routes carry a one-year public
  immutable cache header; the catalog ETag derives from catalog version,
  revocations, and the managed configuration revision.
- Operator policy lives under `policy.*` with per-registry `policy_overrides`;
  `rejected_resources: block|expose` is the closest existing shape and fails
  validation on unknown values.
- Clients before this change enforce only assessment decisions and
  revocation (`enforceMarketplaceUse` in `internal/marketplace/resolver.go`);
  an unknown availability value does not stop them. Any server-side hold must
  therefore be expressed by *what `latest` returns*, not only by metadata.
- Index-only clients never download an artifact; they reconstruct the
  resource from the exact registry occurrence. Collections download a
  manifest, not an archive.

## Goals / Non-Goals

Goals:

- Make a configured period elapse between first publication and resolution as
  `latest`, for every client version.
- Expose first publication and hold state so current clients can apply a
  stricter consumer floor, report holds, and offer initial-acquisition
  acknowledgement.
- Keep every existing Marketplace installation resolvable by exact digest,
  including on a fresh machine after the floor was raised.
- Keep the change schema-free in the metadata store.

Non-Goals:

- Consumer-side behavior (`git-cooldown`, `marketplace-cooldown-client`).
- Database-managed per-registry cooldown and its admin UI (follow-up).
- Hiding held revisions from browsing or history, or denying their
  distribution; a hold is not a revocation.
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
    period: 24h          # duration string or integer nanoseconds, like refresh.interval; default 0 (off); negative is invalid
    held: expose         # expose (default) | strict; any other value is invalid
registries:
  - id: internal-gated
    policy_overrides:
      cooldown:
        period: 0        # per-registry period only; may be lower or higher
```

- `period` accepts the same syntax as `refresh.interval`, so `0` is valid.
- Per-registry overrides carry only `period`; `held` is global. A malformed
  override is ignored with a logged warning and the global applies, matching
  the existing treatment of non-`rejected_resources` overrides. The `held`
  key follows the `rejected_resources` rule instead: an unrecognized value
  fails the whole configuration, so a typo can never weaken the policy.
- `held: expose` (default): held revisions are acknowledgeable; a conforming
  client may pin one for an initial acquisition after explicit
  acknowledgement. `held: strict`: held revisions are marked
  `acknowledgeable: false`; a conforming client refuses the acknowledgement.
  Neither value gates distribution (D4).
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
   it. Revocation, assessment, and license outcomes are overlaid on it as
   today, so a client sees the same refusal it sees now for a head in that
   state; the head's meaning as "what the registry currently publishes" is
   preserved.
3. Otherwise return the revision of that coordinate with the most recent first
   publication at or before `now − P` whose overlay is `available` or
   `operator-exposed`. Revisions whose overlay is `revoked`, `policy-blocked`,
   `blocked`, or `no-artifact` are skipped: they are historical revisions, not
   the head, and skipping them is what keeps an older safe revision
   installable when the newest eligible one was revoked. The response carries
   `held_head: {revision, first_published_at, cooldown_until}` describing the
   head that was withheld, so clients can report the hold and tell a fallback
   from a revert.
4. Otherwise respond `409 Conflict` with body
   `{"error": "...", "code": "marketplace.cooldown.held", "revision": "sha256:…", "first_published_at": "...", "cooldown_until": "...", "acknowledgeable": true|false}`
   where `revision` is the head revision. `409` is already part of the API's
   error vocabulary; `404` is reserved for coordinates that do not exist, and
   clients translate it into "remove the pin".

The store gains `GetLatestEligibleResourceRevision(ctx, coord, cutoff)` on
both drivers returning candidates in first-publication order so the handler
can apply the overlay filter; step 2 deliberately prefers the head when
eligible rather than the globally newest eligible revision, so a revert to
old content resolves to the head.

### D4: Availability overlay, the `cooldown` object, and distribution

`evaluateAvailability` gains a cooldown input (effective period, `held`
mode, `now`). When `P > 0` every `current_availability` carries
`cooldown: {held, first_published_at, cooldown_until, acknowledgeable}`;
`held` is `first_published_at > now − P`, `acknowledgeable` is
`held == false || mode == expose`. The `availability` word is set to
`cooling-down` only when the revision is held and no stronger state applies.
Precedence, highest first: `revoked`, `no-artifact`,
`policy-blocked`/`operator-exposed`, `blocked`, `cooling-down`, `available`.
Clients must read the hold from `cooldown.held`, never from the availability
word, so an `operator-exposed` held revision is still recognised as held.

A hold never gates distribution. Artifact, archive, content, manifest,
inspection, and history routes serve held revisions exactly as they serve
available ones, because:

- a Marketplace installation pinned before the floor was raised must keep
  resolving by exact digest on a fresh machine (ADR 0011 "pins are never
  re-evaluated");
- index-only acquisition never downloads an artifact and collections download
  a manifest, so an artifact gate could not express the policy anyway;
- revocation already provides hard denial with provenance.

In database-management mode `managedResourceAllowed` treats `cooling-down` as
readable and distributable so revision lists, exact reads, and existing
installations are unaffected by a hold; its existing non-cooldown refusals are
retained.

Nothing is reconciled or stored; the overlay is a pure function of
`occurred_at`, policy, and the clock, so changing the period takes effect on
the next request after a configuration reload.

### D5: Caching and validators

- The latest-revision route emits no immutable cache header; any
  `Cache-Control: max-age` must not exceed the time until that coordinate's
  next eligibility boundary. The `409` body is `Cache-Control: no-store`.
- Digest-addressed routes keep `public, max-age=31536000, immutable`; nothing
  they serve depends on the hold (D4).
- The catalog projection, search, and revision-list validators incorporate
  the most recent eligibility boundary already crossed:
  `MAX(first_published_at + P)` over rows with `first_published_at + P ≤ now`,
  computed with one aggregate per request alongside the existing inputs.
  A conditional request therefore misses once a row becomes eligible instead
  of being answered `304` with a stale `cooling-down` state. Responses that
  contain a held row carry `Cache-Control: max-age` bounded by the earliest
  pending boundary.

### D6: Search and catalog projection

Rows keep anchoring on the registry head. They gain `first_published_at` and
the `cooldown` object, so Backstage and the web UI can distinguish a held head
from an available one. Search ranking is unchanged.

### D7: Web UI

The revision list and exact revision pages render a "Held until <date>" badge
from `current_availability.cooldown` when `held` is true. The latest page
needs no badge: `latest` is the eligible revision by construction. The badge
copy avoids the words blocked, revoked, and rejected.

### D8: Codes and logs

- Configuration: existing `marketplace/config` validation errors, extended for
  `policy.cooldown.period` (non-duration or negative) and `policy.cooldown.held`
  (unknown value).
- API error code: `marketplace.cooldown.held` (D3 step 4).
- API log: one debug line per `409` and per `held_head` fallback with
  coordinate, head revision, and `cooldown_until`; no new metrics series.

### D9: Mixed client versions

- Old client, new server, `P > 0`: `latest` returns an eligible revision
  (possibly older than the head) or `409`. The client installs the eligible
  revision unaware of the hold, or fails with
  `resolve latest resource revision: 409 …` for a coordinate with no eligible
  revision. Existing pins and `sync --locked` are unaffected because exact
  reads and distribution are not gated. This is operator opt-in behavior of
  the same kind as revocation, not a change to a supported client contract;
  the released-client gate witnesses both outcomes.
- New client, old server: covered by `marketplace-cooldown-client`
  (`missing_evidence`).

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
  - Scenario: Held head resolves to the newest eligible revision with held_head evidence
  - Scenario: Revoked eligible revision is skipped in the fallback
  - Scenario: Coordinate with no eligible revision answers 409 with cooldown evidence
  - Scenario: Zero period preserves head resolution
  - Scenario: Registry override lowers the period
- Requirement: Held revisions report the cooldown object
  - Scenario: Held available revision reports cooling-down and cooldown_until
  - Scenario: Held operator-exposed revision keeps its state and reports held
  - Scenario: Strict mode reports non-acknowledgeable
- Requirement: Holds never gate distribution
  - Scenario: Held artifact, manifest, and content remain downloadable by exact digest
  - Scenario: Managed metadata reads accept held revisions
  - Scenario: Existing pin inside a raised floor resolves on a fresh cache
- Requirement: Cooldown configuration is validated
  - Scenario: Unknown held value fails configuration
  - Scenario: Negative period fails configuration
  - Scenario: Malformed registry override is ignored with a warning
- Requirement: Caching respects eligibility boundaries
  - Scenario: Latest response carries no immutable cache header
  - Scenario: Catalog validator changes when a row becomes eligible

`marketplace-web-ui`

- Requirement: Held revisions are badged
  - Scenario: Revision list shows the eligible date for a held revision

## Risks / Trade-offs

- **Old clients on a coordinate with no eligible revision see an error, not a
  hold.** Accepted: failing closed is the purpose, the body is
  machine-readable, and existing pins keep working.
- **Query cost.** `MIN(occurred_at)` per revision and one boundary aggregate
  per list request add to projection reads. Both drivers already join
  occurrences; the catalog performance benchmarks in
  `marketplace-catalog-performance` bound the regression and must not move
  beyond their existing budgets.
- **Clock skew between API processes.** Eligibility is evaluated with the
  serving process's clock; two processes can disagree for the skew duration.
  Acceptable for periods measured in hours.
- **`held: strict` is advisory.** A modified client can ignore
  `acknowledgeable: false`. The alternative (denying distribution) breaks
  existing pins and cannot cover index-only acquisition, so strictness is a
  policy signal and revocation remains the enforcement tool.
- **Newly approved registries are fully held.** Accepted in the design
  session; the per-registry override is the escape hatch and the operator
  guide says so.

## Migration Plan

1. Land store projections, the eligible-revision query, and the boundary
   aggregate with tests.
2. Land configuration, availability overlay, latest handler, validators,
   managed read gate, OpenAPI, and the public-gate E2E scenario.
3. Land the web UI badge.
4. Update `site/docs/reference/marketplace-operator-configuration.md`,
   `docs/marketplace-operator.md` (newly approved registries, fresh
   databases, what older clients see), and `docs/marketplace-security.md`.
5. Archive the change in the same pull request.

No data migration. Rollback is a configuration change (`period: 0`).

## Open Questions

- Whether `409` or `423` better signals "exists, not yet eligible". `409` is
  chosen because it is already in the API's vocabulary; reviewers may prefer
  otherwise before the OpenAPI delta is frozen.
- Whether search should optionally exclude held heads for consumers that only
  want installable results (`?eligible=true`). Deferred until a consumer asks.
