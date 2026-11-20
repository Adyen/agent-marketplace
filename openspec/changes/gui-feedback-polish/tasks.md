# Tasks

## 1. Behavioral witnesses

- [x] 1.1 Add Vitest witnesses bound to `gui-shell/sidebar-offers-each-navigation-control-once`, `gui-shell/counts-render-as-plain-neutral-numbers` and `gui-shell/installed-resource-offers-a-labeled-uninstall`, and update the grouping witnesses for `gui-shell/every-valid-grouping-combination-retains-distinct-resources`; verify they fail before the implementation.
- [x] 1.2 Add a local API Go witness bound to `gui-local-api/installed-and-available-marketplace-rows-share-a-registry-label` and update the notification table for `gui-local-api/resource-notifications-name-action-and-resource`; verify they fail before the implementation.

## 2. Implementation

- [x] 2.1 Consolidate the sidebar navigation, Kind list counts, and grouping section list.
- [x] 2.2 Render plain neutral Sync, status bar, and Update counts.
- [x] 2.3 Rename Remove to Uninstall in the inspector, dialog, progress, and notifications; replace the trash icon with text buttons and restyle the inspector actions.
- [x] 2.4 Derive the shared Marketplace registry label for installed, pending, and discovered rows.
- [x] 2.5 Update `gui/DESIGN.md` and every GUI unit and Playwright test that names a changed control or message.

## 3. Verification

- [ ] 3.1 Run `vue-tsc`, Vitest, the affected Playwright specs, the affected Go packages, and `make spec-check`.
- [ ] 3.2 Archive `gui-feedback-polish` as the final commit.
