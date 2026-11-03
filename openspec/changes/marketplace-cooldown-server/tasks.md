## 0. Preconditions

- [ ] 0.1 Remove `skip_specs: true` from `.openspec.yaml` and add the
  `specs/marketplace-api/spec.md` and `specs/marketplace-web-ui/spec.md`
  deltas outlined in `design.md`, in the same pull request as the tests that
  annotate every scenario. Verify
  `npx --yes @fission-ai/openspec@1.12.0 validate marketplace-cooldown-server --strict`
  and `go run ./tools/specaudit trace` pass

## 1. Store projections

- [ ] 1.1 Add `first_published_at` to the assessed-revision projection on
  SQLite and PostgreSQL as `MIN(occurred_at)` over the revision's occurrences,
  including search and catalog projection reads. Red tests first: earliest
  occurrence wins, retry keeps the original time, archive-digest repair keeps
  it. Annotate `marketplace-api/first-publication-is-the-earliest-containing-occurrence`,
  `marketplace-api/republished-identical-content-keeps-its-first-publication`,
  `marketplace-api/archive-digest-repair-keeps-first-publication`
- [ ] 1.2 Add `ListEligibleOccurrences(ctx, coord, cutoff)` to both drivers
  returning occurrences most recent first with each revision's first
  publication, and the eligibility-boundary aggregate used by validators
  (design D5). Cover the no-eligible case, a coordinate whose head is older
  content republished after a newer revision, and the `A → B → A → C`
  rollback history
- [ ] 1.3 Run the `marketplace-catalog-performance` benchmarks before and after
  1.1 and 1.2 and record the delta in the pull request; keep within the
  existing budgets

## 2. Operator configuration

- [ ] 2.1 Decode and validate `policy.cooldown.period`, `policy.cooldown.held`,
  and `registries[].policy_overrides.cooldown.period` (design D2). Red tests:
  unknown `held` fails, negative period fails, malformed override is ignored
  with a warning and the global applies. Annotate
  `marketplace-api/unknown-held-value-fails-configuration`,
  `marketplace-api/negative-period-fails-configuration`,
  `marketplace-api/malformed-registry-override-is-ignored-with-a-warning`
- [ ] 2.2 Carry the global into `admin.DistributionPolicy` at adoption and read
  it from the managed snapshot in database mode; extend the adoption and
  validation tests

## 3. API behavior

- [ ] 3.1 Extend `evaluateAvailability` with the `cooldown` object and the
  `cooling-down` word (design D4 precedence), including the
  `operator-exposed` + held case and `held: strict`. Annotate
  `marketplace-api/held-available-revision-reports-cooling-down-and-cooldown-until`,
  `marketplace-api/held-operator-exposed-revision-keeps-its-state-and-reports-held`,
  `marketplace-api/strict-mode-reports-non-acknowledgeable`
- [ ] 3.2 Implement latest-revision resolution (design D3) including the
  occurrence-ordered fallback, the overlay filter, the transitive collection
  member check, `held_head`, and the `409` body with
  `marketplace.cooldown.held`. Annotate
  `marketplace-api/eligible-head-resolves-as-latest`,
  `marketplace-api/held-head-resolves-to-the-most-recent-eligible-occurrence-with-held-head-evidence`,
  `marketplace-api/strict-mode-marks-held-head-non-acknowledgeable`,
  `marketplace-api/rolled-back-revision-is-not-restored-by-the-fallback`,
  `marketplace-api/revoked-eligible-revision-is-skipped-in-the-fallback`,
  `marketplace-api/collection-with-a-held-member-is-held`,
  `marketplace-api/coordinate-with-no-eligible-revision-answers-409-with-cooldown-evidence`,
  `marketplace-api/young-revoked-head-is-reported-non-acknowledgeable-with-its-availability`,
  `marketplace-api/young-artifact-less-head-is-reported-non-acknowledgeable-with-its-availability`,
  `marketplace-api/zero-period-preserves-head-resolution`,
  `marketplace-api/registry-override-lowers-the-period`
- [ ] 3.3 Prove holds never gate distribution or managed metadata reads:
  artifact, manifest, content, inspection, revision list, and exact reads of
  a held revision in file and database mode; `managedResourceAllowed` accepts
  `cooling-down`; an existing v4 pin inside a raised floor resolves with an
  empty client cache. Annotate
  `marketplace-api/held-artifact-manifest-and-content-remain-downloadable-by-exact-digest`,
  `marketplace-api/managed-metadata-reads-accept-held-revisions`,
  `marketplace-api/existing-pin-inside-a-raised-floor-resolves-on-a-fresh-cache`
- [ ] 3.4 Cache headers and validators (design D5): `no-cache` plus a
  policy- and eligibility-aware ETag on the latest route, `no-store` on the
  `409` body, boundary- and policy-aware ETags on catalog, search, and
  revision list, bounded `max-age` when a held row is present. Annotate
  `marketplace-api/latest-response-revalidates-on-every-use`,
  `marketplace-api/latest-validator-changes-when-the-floor-is-raised`,
  `marketplace-api/catalog-validator-changes-when-a-row-becomes-eligible`,
  `marketplace-api/search-validator-changes-when-a-row-becomes-eligible`,
  `marketplace-api/revision-list-validator-changes-when-a-row-becomes-eligible`,
  `marketplace-api/list-validators-change-when-the-global-period-changes`,
  `marketplace-api/list-validators-change-when-a-registry-override-changes`
  (each sends `If-None-Match` before and after the boundary or policy change
  and expects a changed validator and a fresh body, on catalog, search, and
  revision list)
- [ ] 3.5 Update `api/openapi.yaml`: `first_published_at`, the
  `current_availability.cooldown` object, `held_head`, the `cooling-down`
  availability value, and the `409` response on the latest-revision route.
  Run the OpenAPI conformance tests

## 4. Web UI

- [ ] 4.1 Render the held badge and eligible date on revision list and exact
  revision pages from `current_availability.cooldown`. Annotate
  `marketplace-web-ui/revision-list-shows-the-eligible-date-for-a-held-revision`
  in the Nuxt test suite

## 5. E2E and documentation

- [ ] 5.1 Extend `TestMarketplacePublicGate` with a held head: a v4 client's
  `install` of a coordinate with no eligible revision fails closed on `409`,
  `install` of a coordinate with an eligible older revision installs that
  revision, and `sync --locked` of a pin inside the raised floor succeeds on
  an empty cache. Keep the witnesses required
- [ ] 5.2 Add a released-client gate scenario running the pinned stable client
  against a cooldown-enabled server to witness the same three outcomes, plus
  the documented step-back: a pin inside a newly raised floor followed by
  `update` moves to the most recent eligible occurrence
- [ ] 5.3 Document `policy.cooldown` and the override in
  `site/docs/reference/marketplace-operator-configuration.md`; add the
  newly-approved-registry, fresh-database, and older-client consequences
  (upgrade clients before enabling a non-zero floor, or accept the `update`
  step-back) to `docs/marketplace-operator.md`; mention the recommended 24-hour period and
  that `held: strict` is advisory in `docs/marketplace-security.md`

## 6. Validate and archive

- [ ] 6.1 Run `make validate`, `make spec-check`, the Marketplace public gate,
  and the released-client gate; attach the benchmark delta from 1.3
- [ ] 6.2 `openspec archive marketplace-cooldown-server --yes` in the same pull
  request
