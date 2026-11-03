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

- [ ] 1.1 Parse `first_published_at` and `current_availability.cooldown_until`
  on latest, list, and exact revision responses; parse the `409`
  `marketplace.cooldown.held` body into a typed error; carry
  `first_published_at` in the revision cache. Table tests against recorded
  responses, including a response without the field

## 2. Selection and holds

- [ ] 2.1 Implement the shared candidate-selection helper (design D2) and use
  it from `prepareLocator`, the update planner, and the TUI. Annotate
  `resource-installation/young-latest-resolves-to-the-newest-eligible-older-revision`,
  `resource-installation/server-held-coordinate-with-an-eligible-older-revision-installs-it`,
  `resource-installation/explicit-revision-pins-skip-the-cooldown`
- [ ] 2.2 Hold reporting in install, update, and `update --dry-run`; diff
  exclusion. Annotate
  `resource-installation/install-reports-the-held-head-when-it-selects-an-older-revision`,
  `update-output/held-pin-prints-the-hold-line-and-exits-0`,
  `update-output/held-pins-are-excluded-from-content-diffs`,
  `update-preview/dry-run-reports-the-same-marketplace-hold-as-the-applying-run`,
  `deployed-acquisition-locks/held-update-leaves-the-marketplace-lock-record-byte-identical`
- [ ] 2.3 `missing_evidence` behavior (design D5). Annotate
  `resource-installation/warn-proceeds-and-reports-once-per-endpoint`,
  `resource-installation/refuse-fails-selection-for-the-endpoint`

## 3. Acknowledgement

- [ ] 3.1 Add `install --acknowledge-cooldown`, the
  `cooldown.acknowledgement-required` refusal, strict refusal, and the exact
  fetch-and-pin path under `expose`; prove fail-closed under `block`. Annotate
  the four `resource-installation` acknowledgement scenarios
- [ ] 3.2 Writing `sync` refusal for a held initial Marketplace acquisition
  pointing at the install command. Annotate
  `deployed-acquisition-locks/writing-sync-refuses-a-held-initial-marketplace-acquisition-and-points-at-install`
- [ ] 3.3 Extend `TestPublicCommandPathsHaveBehaviorTests` witnesses for the
  new flag

## 4. TUI

- [ ] 4.1 Detail view "Held until" (design D7); update result holds
- [ ] 4.2 Acknowledgement confirm modal in the Marketplace install flow;
  strict mode shows the diagnostic without a confirm action
- [ ] 4.3 Extend `tools/tui-qa/tests/marketplace-stub.ts` with
  `first_published_at` and the `409` outcome; add
  `marketplace-install-acknowledge-cooldown.spec.ts` and
  `marketplace-update-cooldown-hold.spec.ts`; list them in
  `tools/tui-qa/README.md`; confirm each fails without the change. Annotate
  the three `tui-interactions` and one `tui-feedback` scenarios

## 5. E2E and documentation

- [ ] 5.1 Extend `TestMarketplacePublicGate` with a consumer floor above the
  server floor and an acknowledged initial acquisition; add declarative E2E cases
  with spec tags; run `make validate-e2e`
- [ ] 5.2 Document Marketplace cooldown behavior, the flag, and
  `missing_evidence` in `site/docs/how-to/install-from-a-marketplace.md`,
  `site/docs/reference/configuration.md`, and `lockfiles.md`

## 6. Validate and archive

- [ ] 6.1 Run `make validate`, `make spec-check`, `make tui-qa`, and
  `make validate-e2e`
- [ ] 6.2 `openspec archive marketplace-cooldown-client --yes` in the same
  pull request
