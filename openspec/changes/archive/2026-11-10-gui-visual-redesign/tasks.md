# Tasks

## 1. Update availability

- [x] 1.1 Add failing tests bound to `gui-shell/update-availability-check-writes-nothing` and `gui-shell/update-availability-result-from-before-a-later-change-is-discarded` (Go, `internal/session` or `internal/localapi`); verify they fail on the base
- [x] 1.2 Move the update preview comparison core into a shared internal package used by the CLI and the session; existing CLI update preview tests pass unchanged
- [x] 1.3 Add the user-scope update availability check, its triggers, and the `availableUpdates` snapshot field in `api/gui-local.yaml`, `internal/localapi`, and the regenerated client; tests from 1.1 pass and the API drift test passes
- [x] 1.4 Add failing tests bound to `gui-shell/update-shows-the-available-update-count` and `gui-shell/no-update-indicator-at-zero-before-a-check-or-after-a-failed-check` (Vitest, plus a Playwright spec against a harness fixture with a newer Git revision), then move Update before Sync and render the count badge; tests pass

## 2. Resizable panes

- [x] 2.1 Add failing tests bound to `gui-shell/pointer-drag-resizes-and-clamps-a-pane`, `gui-shell/keyboard-resizes-a-pane-from-its-handle`, `gui-shell/pane-widths-persist-across-relaunch`, and `gui-shell/missing-or-malformed-stored-pane-width-uses-the-default`
- [x] 2.2 Add the sidebar and inspector resize handles with clamping and GUI-local persistence; tests from 2.1 pass

## 3. Resource action dialog

- [x] 3.1 Add failing tests bound to `gui-shell/resource-action-dialog-uses-plain-language` and `gui-shell/spinner-shows-only-while-the-action-is-in-flight`
- [x] 3.2 Remove the dialog icon, add the plain-language text, the collapsed "Technical details" disclosure, and the spinner; update existing Vitest and Playwright tests that read the preview; tests pass

## 4. Presentation

- [x] 4.1 Restyle the Settings "Add source" button with the secondary button style; capture light and dark screenshots
- [x] 4.2 Add the icon source and regenerate `AppIcon.icns` and every hicolor PNG size; `make gui-build` succeeds; capture screenshots of the new icon
- [x] 4.3 Update `gui/DESIGN.md` for the toolbar order and update badge, resizable panes, the resource action dialog, the Add source button, and the icon

## 5. Verification

- [x] 5.1 `make validate`, `make spec-check`, and in `gui/`: `vue-tsc --noEmit`, `vitest run`, `vite build`, `playwright test` all pass

## Workflow follow-up

- `openspec archive gui-visual-redesign --yes`
