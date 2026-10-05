## Context

The interactive TUI resolves its startup snapshot through a progressive
pipeline: validated configuration publishes provisional verification-pending
rows and launches cached-first Marketplace discovery, exact-lock and catalog
resolution publish a verified inventory, and the dry-run assessment
publishes the final authoritative snapshot. When an organization Marketplace
has no deployed lock, a browse-safe configuration is published while
transient acquisition is still blocked. This baseline records that existing
observable behavior; the sole source of claims is
`docs/tui-product-contract.md` and the behavior proven by existing tests in
`internal/tui`, `internal/marketplace`, and `internal/pipeline`.

## Decisions

- **One capability, `tui-startup`, scoped to observable staging.** The
  requirements describe only what a user or downstream system observes:
  stage ordering, pending-row rendering, browse-safe publication,
  toggle/mutation gating, and generation isolation. They do not name Go
  identifiers, package layout, or function names.
- **No policy duplication.** Selection precedence, exclude semantics,
  acquisition-lock policy, transient-acquisition correctness, and
  configuration layering are specified by `config-layering`,
  `acquisition-lock-codec`, `marketplace-api`, and
  `owned-entry-reconciliation`. This capability references those outcomes
  ("validated configuration", "well-known-verified endpoint identity")
  without restating their policies.
- **Every scenario is bound to a pre-existing passing test.** The delta adds
  no new test logic; each scenario carries at least one
  `// spec: tui-startup/<slug>` annotation on the existing test that already
  asserts its WHEN/THEN.

## Scenario-to-test map

| Scenario slug | Test |
|---|---|
| startup-snapshot-delivers-config-inventory-then-assessed-stages | `internal/tui` `TestReadSnapshotStagesDeliversConfigInventoryThenAssessed` |
| invalid-configuration-delivers-one-fail-closed-final-stage | `internal/tui` `TestReadSnapshotStagesInvalidConfigStaysFailClosed` |
| config-stage-starts-cached-first-marketplace-discovery-before-local-verification-finishes | `internal/tui` `TestConfigStageStartsDiscoveryBeforeLocalVerification` |
| provisional-config-stage-publishes-verification-pending-rows | `internal/tui` `TestProvisionalConfigStagePublishesPendingRows` |
| excluded-configured-selections-render-unchecked-in-their-verified-position | `internal/tui` `TestProvisionalStageDoesNotCheckExcludedMarketplacePin` |
| local-all-true-selections-expand-into-pending-rows-on-the-normal-config-path | `internal/tui` `TestProvisionalLocalAllTrueRowsRenderOnNormalConfigPath` |
| provisional-rows-render-while-transient-acquisition-is-blocked | `internal/tui` `TestProvisionalLocalAllTrueRowsRenderBeforeBlockedAcquisition`, `TestProvisionalConfiguredRowsRenderBeforeBlockedAcquisition` |
| registry-scoped-local-excludes-apply-to-provisional-rows | `internal/tui` `TestProvisionalRegistryScopedLocalExcludesApply` |
| excluded-only-configuration-publishes-no-provisional-rows | `internal/tui` `TestExcludedOnlyProvisionalStagePublishesNoRows` |
| provisional-layout-matches-verified-inventory | `internal/tui` `TestProvisionalLayoutMatchesVerifiedInventory` |
| optional-collection-member-yields-to-a-configured-selection-of-the-same-resource | `internal/tui` `TestProvisionalCollectionMemberYieldsToConfiguredSelectionOfSameName` |
| registry-selections-carry-the-harness-override-badge | `internal/tui` `TestProvisionalRowsCarryRegistryOverrideHarnessBadge` |
| organization-marketplace-collection-expands-from-the-verified-cached-manifest | `internal/tui` `TestProvisionalStageExpandsOrganizationCollectionFromVerifiedCache` |
| authoritative-failure-clears-provisional-rows | `internal/tui` `TestAuthoritativeFailureClearsProvisionalRows` |
| browse-safe-config-publishes-the-verified-endpoint-before-acquisition | `internal/pipeline` `TestBrowsingConfigReadyPublishesVerifiedEndpointBeforeAcquisition` |
| provisional-config-publishes-configured-pins-before-acquisition | `internal/pipeline` `TestProvisionalConfigReadyPublishesPinsBeforeAcquisition` |
| browse-safe-config-is-never-published-for-unsafe-configurations | `internal/pipeline` `TestBrowsingConfigReadyNeverPublishesUnsafeConfig` |
| cached-marketplace-rows-render-while-acquisition-is-blocked | `internal/tui` `TestBrowsingConfigStageRendersCachedRowsBeforeBlockedAcquisition` |
| verified-inventory-is-navigable-and-toggle-ready-while-drift-is-pending | `internal/tui` `TestInventoryStageIsNavigableAndToggleReadyWithDriftPending` |
| drift-applies-only-to-rows-of-its-own-catalog-selection | `internal/tui` `TestApplyDriftItemsMarksOnlySelectedRows` |
| final-stage-replaces-pending-inventory-with-the-assessment | `internal/tui` `TestFinalStageReplacesPendingInventoryWithAssessment` |
| final-stage-merges-discovery-published-during-the-config-stage | `internal/tui` `TestFinalStageMergesDiscoveryPublishedDuringConfigStage` |
| verified-rows-inserted-above-a-selection-keep-it-on-the-same-screen-line | `internal/tui` `TestVerifiedRowsInsertedAboveSelectionKeepItOnTheSameScreenLine` |
| an-untouched-cursor-stays-on-top-when-installed-rows-arrive | `internal/tui` `TestUntouchedCursorStaysOnTopWhenInstalledRowsArrive` |
| an-open-detail-follows-its-verified-row | `internal/tui` `TestPendingDetailFollowsVerifiedRow` |
| superseded-stage-delivery-cannot-overwrite-newer-state | `internal/tui` `TestSupersededStageDeliveryCannotOverwriteNewerState` |
| a-stale-provisional-stage-cannot-publish-rows | `internal/tui` `TestStaleProvisionalStageCannotPublishRows` |
| an-intermediate-stage-cannot-complete-a-pending-mutation-reload | `internal/tui` `TestInventoryStageDoesNotCompletePendingToggleReload` |
| mutations-stay-gated-until-verified-inventory | `internal/tui` `TestSyncKeyBlockedBeforeVerifiedInventory`, `TestDiscoveryRowCannotInstallBeforeVerifiedInventory` |
| cached-collection-leaves-prefer-the-exact-then-newest-verified-revision | `internal/marketplace` `TestCachedCollectionLeavesPrefersExactThenNewestVerifiedRevision` |
| tampered-cached-manifests-are-rejected | `internal/marketplace` `TestCachedCollectionLeavesRejectsTamperedManifest` |

## Discarded claims

No legacy documents are harvested by this change; the harvest ledger is
untouched. Claims in `docs/tui-product-contract.md` not carried into this
capability, with reasons:

- Search debounce, remote search supersession, and the 50-result first-page
  cap: general Marketplace-browsing behavior exercised independently of
  startup staging; no dedicated binding test was produced for this delta.
- Detail-pane replaceable rendering, mutation reload semantics beyond the
  pending-toggle gate, toast/diagnostic lifecycles, and the split bundle
  browser: unrelated to startup staging; asserting them here would duplicate
  other capabilities' scope.
- Benchmark milestone names (`config_ready_prerequisite`, `first_visible`,
  `toggle_ready`, `fully_assessed`): performance evidence conventions, not
  user-observable behavior.
