## Why

A compromised registry publishes a malicious resource revision and every
consumer that runs `update`, or installs the resource for the first time,
receives it as soon as the next refresh indexes it. Revocation can stop
distribution only after someone has noticed. A cooldown gives operators that
window by default: a Marketplace stops resolving a resource revision as
`latest` until it has been published for a configured period, measured by the
one clock a registry compromiser cannot rewrite, the Marketplace's own first
publication time.

This is the server half of the cooldown design recorded in
[ADR 0011](../../../docs/adr/0011-cooldown-clocks-and-history-rewrite-refusal.md).
The client halves are `git-cooldown` and `marketplace-cooldown-client`.

## What Changes

- Marketplace operator configuration gains `policy.cooldown` with `period`
  (default `0`, off) and `held` (`expose`, the default, or `strict`), and a
  per-registry `policy_overrides.cooldown.period`. The operator owns the floor,
  so a registry override may lower it. Unknown `held` values fail validation.
- Every resource revision response (`latest`, revision list, exact revision,
  search, catalog projection) carries `first_published_at`: the earliest
  `occurred_at` among the registry occurrences that contain the revision.
  Existing rows already have this evidence; no backfill is needed.
- `current_availability` gains a `cooldown` object whenever a period applies:
  `held`, `first_published_at`, `cooldown_until`, and `acknowledgeable`
  (`false` under `held: strict`). A held revision that is otherwise available
  reports `availability: cooling-down`; a held revision that is revoked,
  rejected, or missing its artifact keeps reporting that stronger state, and
  clients read the hold from the `cooldown` object, not from the availability
  word.
- The latest-revision route never returns a held candidate. It returns the
  registry-head revision when that revision is eligible; otherwise the most
  recently published eligible revision that is not revoked, rejected,
  blocked, or artifact-less (ordered by registry occurrence, so a rolled-back
  revision is never restored), together with a `held_head` object describing
  the head it withheld, its availability, and whether it is acknowledgeable; otherwise `409 Conflict` with a machine-readable body
  naming the head revision and its `cooldown_until`. A collection revision is
  eligible only when every member it selects, transitively, is eligible under
  its own registry's period. Clients that predate this change therefore fail
  closed on a coordinate with no eligible revision instead of installing it.
- A hold is not a distribution control. Artifact, archive, content, manifest,
  and inspection routes serve held revisions exactly as before, so a
  Marketplace installation pinned before a floor was raised still resolves by
  exact digest on a fresh machine, and index-only acquisition is unaffected.
  `held: strict` only marks held revisions non-acknowledgeable for conforming
  clients; hard denial remains revocation's job.
- Latest-revision responses revalidate on every use (`no-cache` with a
  policy- and eligibility-aware validator), and the catalog and revision-list
  validators change when a revision crosses its eligibility boundary or the
  floor changes, so caches cannot pin a stale answer across a policy change.
  Digest-addressed routes keep their immutable caching.
- Metadata reads in database-management mode (revision list, exact revision,
  history, inspection) accept held revisions; the managed access gate treats
  `cooling-down` as readable.
- The Nuxt web UI badges held revisions and shows the eligible date on the
  revision list and exact revision pages.
- In database-management mode the YAML `policy.cooldown` global is carried
  into the managed snapshot at adoption like `rejected_resources`; the
  per-registry override is file-mode only in this change. A follow-up change
  adds the database-managed per-registry field and its admin API/UI.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `marketplace-api`: `first_published_at` on revision responses; the
  `current_availability.cooldown` object and `cooling-down` availability;
  latest-revision resolution under the server floor, `held_head`, and the
  `409` outcome; `held: expose|strict`; operator configuration validation for
  `policy.cooldown` and `policy_overrides.cooldown`; caching and validators
  across eligibility boundaries; managed metadata reads of held revisions.
- `marketplace-web-ui`: held-revision badge and eligible date on revision
  pages.

## Impact

- `pkg/marketplace/config`: `policy.cooldown` and per-registry override
  decoding and validation; defaults; documentation in
  `site/docs/reference/marketplace-operator-configuration.md`.
- `pkg/marketplace/store` (SQLite and PostgreSQL): first-publication
  projection on assessed revisions, a latest-eligible query, and an
  eligibility-boundary aggregate for validators; no schema migration, because
  `registry_occurrences.occurred_at` already exists.
- `pkg/marketplace/api`: availability evaluation gains the cooldown object;
  latest-revision handler; validators; `managedResourceAllowed` accepts
  `cooling-down`; `api/openapi.yaml` schema additions.
- `pkg/marketplace/admin`: adoption carries the YAML global into the managed
  snapshot's distribution policy.
- `web/`: badge and date on revision pages.
- E2E: the public gate (`TestMarketplacePublicGate`) gains a held-head
  scenario proving a v4 client fails closed on `409`, installs an eligible
  older revision when one exists, and keeps resolving an existing pin inside
  a raised floor. The released-client gate runs the pinned stable client
  against a cooldown-enabled server to witness the same two outcomes.
- No change to consumer configuration, acquisition locks, or installed
  resources.

## Release note

Marketplace operators can set `policy.cooldown.period` so newly published
resource revisions are not resolved as latest until they have been published
for that long; held revisions report `cooling-down` with the date they become
eligible, and existing pins keep resolving. Clients older than this release see
an error instead of a hold when every revision of a resource is still young.
The default is off; a 24-hour period is recommended.
