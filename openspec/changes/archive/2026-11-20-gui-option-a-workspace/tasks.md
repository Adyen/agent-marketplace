# Tasks

## 1. Behavioral witnesses

- [x] 1.1 Add the mixed-source facet-intersection workspace test with its `gui-shell/workspace-facets-intersect-and-clear-independently` binding; verify it fails for the missing controls with `./node_modules/.bin/vitest run src/workspace.test.ts -t 'workspace facets|source header'`.
- [x] 1.2 Add source-header metadata, attributable-problem, safe-open, projected-copy, and organization-removal unit witnesses with the matching `gui-shell/source-header-exposes-attributable-problems-and-safe-actions` and `gui-shell/source-shortcut-preserves-removal-confirmation-and-scope` bindings; verify the same focused Vitest command fails for missing behavior.
- [x] 1.3 Add the isolated source-browse Playwright removal journey with its `gui-shell/source-shortcut-preserves-removal-confirmation-and-scope` binding; verify it fails because the header removal shortcut is missing using `./node_modules/.bin/playwright test tests/sources.spec.ts -g 'confirms removal requested from the source header'`.

## 2. Workspace behavior

- [x] 2.1 Implement independently clearable kind/status facets, All resources reset, facet chips, and compact responsive sidebar controls without changing grouping, search generation, counters, or persisted preferences; verify the focused workspace Vitest cases pass.
- [x] 2.2 Add the selected-source header, strict HTTP(S) open guard, projected-location copy status, and shared clipboard helper while preserving diagnostic clipboard behavior; verify the source-header workspace Vitest cases pass.

## 3. Settings routing and design

- [x] 3.1 Route eligible source-header removal through the existing Settings reveal and confirmation flow, revalidating the loaded editable user source by ID; verify the source-header Playwright journey proves cancel leaves bytes unchanged and confirmation removes only that source.
- [x] 3.2 Update `gui/DESIGN.md` for facets, source context/actions, and short-height sidebar scrolling; verify the documentation describes the retained tokens and separate source-list/footer behavior.

## Workflow follow-up

- Archive `gui-option-a-workspace` after the tracked implementation and witness tasks are verified.
- Run the requested `make spec-check` and `make validate` after archiving.
