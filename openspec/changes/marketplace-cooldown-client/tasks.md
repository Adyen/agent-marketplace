## 0. Preconditions

- [ ] 0.1 Confirm `marketplace-cooldown-server` and `git-cooldown` are
  archived (the API fields and the consumer schema exist), then remove
  `skip_specs: true` from `.openspec.yaml` and add the spec deltas outlined in
  `design.md` (`resource-installation`, `deployed-acquisition-locks`,
  `update-output`, `update-preview`, `tui-interactions`, `tui-feedback`) in
  the same pull request as the tests that annotate every scenario. Verify
  `npx --yes @fission-ai/openspec@1.12.0 validate marketplace-cooldown-client --strict`
  and `go run ./tools/specaudit trace` pass

## 1. Client parsing (internal/marketplace)

- [ ] 1.1 Parse `first_published_at`, `current_availability.cooldown`, and
  `held_head` on latest, list, and exact revision responses; parse the `409`
  `marketplace.cooldown.held` body into a typed error that tolerates a
  malformed body; carry the new fields in the revision cache. Table tests
  against recorded responses, including a response without the field and a
  `409` with an unparseable body

## 2. Selection and holds

- [ ] 2.1 Implement the shared candidate-selection helper (design D2) with
  the zero-period bypass, the head/fallback distinction, the
  server-eligibility check on every list candidate, occurrence-ordered
  "newer than the pin", and the transitive collection member check, and use
  it from `prepareLocator`, the update planner, and the TUI. Annotate
  `resource-installation/young-head-resolves-to-the-most-recently-published-eligible-revision`,
  `resource-installation/server-floor-above-the-consumer-floor-is-honored-from-the-revisions-list`,
  `resource-installation/positive-consumer-floor-with-a-zero-server-period-selects-from-the-revisions-list`,
  `resource-installation/server-fallback-published-before-the-pin-is-held-not-installed`,
  `resource-installation/eligible-head-after-a-revert-is-followed`,
  `resource-installation/zero-period-never-compares-timestamps`,
  `resource-installation/collection-with-a-held-member-is-held-under-the-consumer-floor`,
  `resource-installation/explicit-revision-pins-skip-the-cooldown`
- [ ] 2.2 Hold reporting in install, update, and `update --dry-run` using
  `held_head`, the `409` body, or the retained `latest` head as evidence,
  with the eligible date as the later of `first_published_at + P` and the
  server's `cooldown_until`, the unknown-head rendering for an unparseable
  `409`, and diff exclusion. Annotate
  `resource-installation/install-reports-the-held-head-when-it-selects-an-older-revision`,
  `resource-installation/consumer-only-hold-reports-the-latest-head-as-evidence`,
  `update-output/held-pin-prints-the-hold-line-with-the-head-s-eligible-date-and-exits-0`,
  `update-output/unparseable-hold-body-for-an-existing-pin-reports-an-unknown-head`,
  `update-output/collection-hold-reports-the-latest-member-eligible-date`,
  `update-output/held-pins-are-excluded-from-content-diffs`,
  `update-preview/dry-run-reports-the-same-marketplace-hold-as-the-applying-run`,
  `deployed-acquisition-locks/held-update-leaves-the-marketplace-lock-record-byte-identical`
- [ ] 2.3 `missing_evidence` behavior (design D5). Annotate
  `resource-installation/warn-proceeds-and-reports-once-per-endpoint`,
  `resource-installation/refuse-fails-selection-for-the-endpoint`

## 3. Acknowledgement

- [ ] 3.1 Add `install --acknowledge-cooldown`, the
  `cooldown.acknowledgement-required` refusal, the strict and
  server-non-acknowledgeable refusals checked before mirror download,
  index-only clone, and collection expansion, the unknown-head refusal, and
  the exact fetch-and-pin of the server-identified or retained head,
  including after a revert and for a consumer-only hold. Annotate the eight
  `resource-installation` acknowledgement scenarios
- [ ] 3.2 Writing `sync` refusal for a held initial Marketplace acquisition
  pointing at the install command. Annotate
  `deployed-acquisition-locks/writing-sync-refuses-a-held-initial-marketplace-acquisition-and-points-at-install`
- [ ] 3.3 Extend `TestPublicCommandPathsHaveBehaviorTests` witnesses for the
  new flag

## 4. TUI

- [ ] 4.1 Detail view "Held until" from `current_availability.cooldown` and
  the consumer floor (design D7); update result holds
- [ ] 4.2 Acknowledgement confirm modal in the Marketplace install flow; a
  hold that is not acknowledgeable on either side shows the diagnostic
  without a confirm action
- [ ] 4.3 Extend `tools/tui-qa/tests/marketplace-stub.ts` with
  `first_published_at`, the `cooldown` object, `held_head`, and the `409`
  outcome; add `marketplace-install-acknowledge-cooldown.spec.ts` and
  `marketplace-update-cooldown-hold.spec.ts`; list them in
  `tools/tui-qa/README.md`; confirm each fails without the change. Annotate
  the four `tui-interactions` and one `tui-feedback` scenarios

## 5. E2E and documentation

- [ ] 5.1 Extend `TestMarketplacePublicGate` with a consumer floor above the
  server floor, a server floor above the consumer floor (the revisions list
  must not yield a server-held revision), a server fallback older than the
  pin, a revert, and an acknowledged initial acquisition; add declarative
  E2E cases with spec tags; run `make validate-e2e`
- [ ] 5.2 Document Marketplace cooldown behavior, the flag, `missing_evidence`,
  and the no-downgrade rule in
  `site/docs/how-to/install-from-a-marketplace.md`,
  `site/docs/reference/configuration.md`, and `lockfiles.md`

## 6. Validate and archive

- [ ] 6.1 Run `make validate`, `make spec-check`, `make tui-qa`, and
  `make validate-e2e`
- [ ] 6.2 `openspec archive marketplace-cooldown-client --yes` in the same
  pull request
