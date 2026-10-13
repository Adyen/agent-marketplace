## 1. Regression Tests

- [x] 1.1 Add annotated Go tests for the five added model-level scenarios and keep the modified ignored-mouse-events witness passing; verify each scenario has its `// spec: tui-mouse-navigation/<scenario-slug>` witness and run `go run ./tools/specaudit trace`.

## 2. Click Navigation

- [x] 2.1 Implement release-in-press-cell navigation clicks for resource-list rows, section headers, and bundle-tree entries, reusing the keyboard actions and render geometry; verify the focused click, wheel, frame-pacing, and text-selection tests pass.

## 3. Terminal Evidence and Documentation

- [x] 3.1 Add an encoded terminal QA journey covering an inert list drag, a selecting click, an opening click, and the absence of clipboard writes; verify it with `make tui-qa`.
- [x] 3.2 Update the published TUI mouse guidance and the TUI QA journey list.

## 4. Verification and Archive

- [x] 4.1 Run `make validate`, `make spec-check`, and `make tui-qa`.
- [x] 4.2 Run `openspec archive 2026-10-06-tui-mouse-click-navigation --yes`.
