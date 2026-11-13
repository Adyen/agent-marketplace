# Tasks

## 1. AC2: Command-R refresh

- [x] 1.1 Update Refresh-button tests to use Command-R. Keep all three tags: `// spec: gui-shell/refresh-and-focus-reload-use-ordinary-snapshot-behavior`, `// spec: gui-shell/external-file-changes-require-a-refresh-trigger`, and `// spec: gui-shell/all-actions-are-keyboard-reachable-and-named`. Run the assertions before UI changes; verify refresh, focus reload, and post-mutation reload behavior.
- [x] 1.2 Remove the visible refresh control while retaining Command-R and the existing focus and mutation reload paths; verify the three tagged scenarios pass.
- [x] 1.3 Update the refresh text in `gui/DESIGN.md`; verify it describes Command-R and preserves focus and mutation reload behavior.

## 2. AC1: Grouped-list headers

- [x] 2.1 Reproduce the long status grouping with a Then-by subgroup in the preview in light and dark themes. Add a browser test that fails on the observed header geometry or opacity, and record root-cause evidence in the run evidence directory. If it does not reproduce, report AC1 blocked and leave it unresolved.
- [x] 2.2 After a reproduction, apply the smallest CSS fix supported by the root-cause evidence; verify the browser test passes in both themes. Do not mark AC1 complete without a reproduction and passing test.

## 3. AC3-AC7: Workspace chrome

- [x] 3.1 For AC3, write a failing browser test first, then remove the Space hint; verify the test passes and the other hints remain.
- [x] 3.2 For AC4, write a failing browser test first, then limit inspector metadata to Registry, Scope, and Status; verify the test passes.
- [x] 3.3 For AC5, write a failing browser test first, then set the light and dark scrim alphas to `0.2` and `0.4` with no backdrop blur; verify the test passes.
- [x] 3.4 For AC6, write a failing browser test first, then hide Pending at zero and show its count when positive; verify both states pass.
- [x] 3.5 For AC7, write a failing browser test first, then verify active navigation in both Resources and Diagnostics views; confirm the test passes.
- [x] 3.6 Update `gui/DESIGN.md` for AC3-AC7; verify the design text matches the tested behavior.

## 4. AC8: Diagnostic text and copying

- [x] 4.1 Write failing tests first for exact diagnostic copy text in input order, using one `<severity> <code>: <message>` line per entry; verify native `ClipboardSetText` is preferred, browser `writeText` is the fallback, and "Copied" appears only after a resolved success, not a rejected write.
- [x] 4.2 Write failing GUI tests first for a separately named native expand button that works with Enter and Space and selectable sibling text; implement the component-local behavior without changing the API and verify the tests pass.
- [x] 4.3 Update `gui/DESIGN.md` for AC8; verify it describes input-order copying, both clipboard paths, success feedback, and the separate expand control.

## 5. Integration

- [x] 5.1 Run `cd gui && ./node_modules/.bin/vue-tsc --noEmit && ./node_modules/.bin/vitest run`; verify typechecking and GUI unit tests pass.
- [x] 5.2 Run `cd gui && ./node_modules/.bin/vite build`; verify it updates the embedded frontend at `cmd/agent-marketplace-gui/frontend/dist`.
- [x] 5.3 Run the four contracted Playwright specs with `cd gui && ./node_modules/.bin/playwright test tests/refresh.spec.ts tests/navigation.spec.ts tests/feedback.spec.ts tests/visual-redesign.spec.ts`; verify all four pass.
- [ ] 5.4 Run `make spec-check` and `make validate`; verify both gates pass.

## Workflow follow-up

- openspec archive clean-up-gui-workspace-chrome --yes
