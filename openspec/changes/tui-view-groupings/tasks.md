## 1. Grouped browsing

- [ ] 1.1 Add failing scenario-bound tests for every grouping combination, attention membership/order, source identity collisions, metadata search, folds and selection; verify the focused command fails on the unchanged base for missing behavior.
- [ ] 1.2 Implement the grouping tree and cached search metadata with grouping-specific session folds; verify those focused tests and existing browsing tests pass.

## 2. View preferences

- [ ] 2.1 Add failing scenario-bound tests for modal preview/accept/discard, former tab keys, minimum-size framing, versioned XDG loading and atomic save failures; verify intended red failures before implementation.
- [ ] 2.2 Implement View settings entry points, bounded form, cancellation and versioned per-user persistence; verify preferences survive restart without config/lock access and all relevant Go tests pass.

## 3. Bulk toggles

- [ ] 3.1 Add failing scenario-bound tests for filtered folded descendants, eligibility parity, explicit scope/direction/confirmation, cancellation, failure atomicity, current-target revalidation, one reload, and stale completion; verify intended red failures.
- [ ] 3.2 Reuse the per-resource selection mutation in one staged chosen-layer batch through existing serialized lifecycle and generation guards; verify no partial writes and unchanged single-row behavior.
- [ ] 3.3 Add help/palette entries and one privacy-safe bulk telemetry span; verify discoverability and telemetry tests pass without identifying attributes.

## 4. Integration and publication claims

- [ ] 4.1 Add one direct PTY journey for Source → Kind, folded section bulk disable at project scope, pending-sync feedback and restart persistence; verify the journey runs without skips.
- [ ] 4.2 Reconcile TUI documentation and development verification guidance with the delivered behavior; verify relevant examples and mapped journeys remain reachable.
- [ ] 4.3 Run focused TUI tests, make validate, make spec-check, make tui-qa-check, and make tui-qa; retain zero-exit output and observable terminal evidence, and record baseline/candidate benchmark results.
- [ ] 4.4 Run `openspec archive tui-view-groupings --yes` in this pull request and verify the main-spec sync with strict validation and spec audit.
