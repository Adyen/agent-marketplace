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
  (default `0`, off) and `held` (`expose`, the default, or `block`), and a
  per-registry `policy_overrides.cooldown.period`. The operator owns the floor,
  so a registry override may lower it. Unknown `held` values fail validation.
- Every resource revision response (`latest`, revision list, exact revision)
  carries `first_published_at`: the earliest `occurred_at` among the registry
  occurrences that contain the revision. Existing rows already have this
  evidence; no backfill is needed.
- A revision younger than the effective server floor is a held candidate. It
  carries `availability: cooling-down` and `cooldown_until` in
  `current_availability`. The latest-revision route never returns a held
  candidate: it returns the registry-head revision when that revision is
  eligible, otherwise the eligible revision with the most recent first
  publication, otherwise `409 Conflict` with a machine-readable body naming
  the held revision and its `cooldown_until`. Clients that predate this change
  therefore fail closed on a held coordinate instead of installing it.
- Under `held: expose`, a held revision remains fetchable by exact digest with
  its artifact, so a current client can offer initial-acquisition acknowledgement.
  Under `held: block`, artifact and content distribution for a held revision
  are denied the same way `policy-blocked` denies them; metadata, history, and
  inspection remain readable.
- Search and catalog projection rows continue to anchor on the registry head
  but expose `first_published_at` and the `cooling-down` availability so
  consumers can tell a held head from an available one.
- The latest-revision route stops being cacheable beyond the next eligibility
  boundary; digest-addressed routes keep their immutable caching.
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

- `marketplace-api`: `first_published_at` on revision responses; `cooling-down`
  availability with `cooldown_until`; latest-revision resolution under the
  server floor and its `409` outcome; `held: expose|block` distribution
  semantics; operator configuration validation for `policy.cooldown` and
  `policy_overrides.cooldown`; caching of the latest-revision route.
- `marketplace-web-ui`: held-revision badge and eligible date on revision
  pages.

## Impact

- `pkg/marketplace/config`: `policy.cooldown` and per-registry override
  decoding and validation; defaults; documentation in
  `site/docs/reference/marketplace-operator-configuration.md`.
- `pkg/marketplace/store` (SQLite and PostgreSQL): first-publication
  projection on assessed revisions and a latest-eligible query; no schema
  migration, because `registry_occurrences.occurred_at` already exists.
- `pkg/marketplace/api`: availability evaluation gains the cooldown overlay;
  latest-revision handler; artifact/content gating for `held: block`;
  `api/openapi.yaml` schema additions.
- `pkg/marketplace/admin`: adoption carries the YAML global into the managed
  snapshot's distribution policy.
- `web/`: badge and date on revision pages.
- E2E: the public gate (`TestMarketplacePublicGate`) gains a held-head
  scenario proving a v4 client fails closed on `latest` and can fetch the exact
  revision under `expose`.
- No change to consumer configuration, acquisition locks, or installed
  resources. Marketplace installations pinned to a revision that is later
  inside a raised floor keep resolving by exact digest.

## Release note

Marketplace operators can set `policy.cooldown.period` so newly published
resource revisions are not resolved as latest until they have been published
for that long; held revisions report `cooling-down` with the date they become
eligible. The default is off; a 24-hour period is recommended.
