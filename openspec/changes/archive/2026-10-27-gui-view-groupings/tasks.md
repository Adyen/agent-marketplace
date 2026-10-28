## 1. Snapshot source identity

- [x] 1.1 Add `source` (`id`, `label`) to the snapshot row contract in `api/gui-local.yaml` and regenerate `gui/src/generated/api.d.ts`; keep the contract drift test passing.
- [x] 1.2 Populate `source` in `rowPresentationForUser`; add Go tests for distinct ids and labels across ref and subpath, the local source, and terminal sanitization of the label.

## 2. Grouping model

- [x] 2.1 Add `gui/src/grouping.ts` with axis types, valid Then by choices, view normalization, local storage read and write, and `buildSections`.
- [x] 2.2 Add Vitest coverage for every valid combination, source and kind ordering, attention pinning, empty sections, filtered rank order, and invalid storage fallbacks.

## 3. Workspace navigation

- [x] 3.1 Replace the status-only groups with nested sections in `WorkspaceView.vue`, `ResourceList.vue`, and `Sidebar.vue`, with folds keyed by grouping combination.
- [x] 3.2 Preserve selection on grouping change, keep search folds query-local, and keep all existing ids, aria names, and keyboard handlers.

## 4. View menu

- [x] 4.1 Add `ViewMenu.vue` with the View button, radio fieldsets, immediate apply, Escape dismissal with focus return, outside-click dismissal, and the `v` shortcut.
- [x] 4.2 Add WorkspaceView tests for the View button, the `v` key, Then by exclusions, persistence, no client requests, folds, selection, and search restore.

## 5. Documentation and preview

- [x] 5.1 Update `gui/DESIGN.md` for the View menu, the `v` key, and nested sections.
- [x] 5.2 Add `--view` to `gui/preview/shoot.ts` and attention and duplicate-source rows to the default scenario if missing.

## 6. Verification

- [x] 6.1 Run `vue-tsc`, Vitest, eslint if configured, the Go localapi tests, the Playwright navigation, accessibility, and search specs, and `make spec-check` (blocked: unbound scenarios in other in-flight changes; this change validates and traces clean).
- [x] 6.2 Capture screenshots for the default scenario, `source/kind`, `kind` in dark theme, and the View menu open.
