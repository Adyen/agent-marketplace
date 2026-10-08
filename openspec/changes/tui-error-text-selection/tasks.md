## 1. Regression Tests

- [x] 1.1 Add model and native terminal-transport tests for the four new error-screen scenarios, with the required `// spec: tui-mouse-navigation/<scenario-slug>` bindings; verify the focused selection tests fail on the immutable base because error-screen selection is absent, not because of tooling or fixtures, while existing preview-selection tests pass.

## 2. Error-Screen Selection

- [x] 2.1 Reuse the existing selection engine for the full-width errors body ending above the footer, applying highlight and extraction to the fitted displayed frame; verify forward and backward wrapped-Unicode selection and click, outside-text, and footer clipboard boundaries.
- [x] 2.2 Include the errors pane, terminal dimensions, and viewport offset in selection identity and use existing frame refresh comparison for changed diagnostics; verify stale selection clears on scrolling, resizing, diagnostic changes, and closing, while Escape clears first, `e` closes, and Ctrl+C quits.

## 3. Native Terminal Verification

- [x] 3.1 Verify encoded terminal drag/release input through the existing frame-paced path produces the expected highlight and exact displayed-text clipboard write, with no write for ignored gestures; run `CGO_ENABLED=0 go test ./internal/tui -run '^TestErrorSelection' -count=1`.
- [ ] 3.2 Run `CGO_ENABLED=0 go test ./internal/tui -count=1`, `make validate TEST_PACKAGES=./internal/tui`, `make spec-check`, and `make tui-qa`; verify existing preview selection, wheel routing, frame pacing, and terminal restoration remain passing.

## 4. Archive

- [ ] 4.1 After implementation and verification are complete, run `openspec archive tui-error-text-selection --yes` in the implementation pull request and verify `openspec validate --all --strict` passes.
