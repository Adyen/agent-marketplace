## 1. Spec

- [x] 1.1 Add `gui-shell` requirement "Status bar keyboard hints appear on keyboard use" with its scenario.

## 2. Implementation and witnesses

- [x] 2.1 Show the status bar keyboard hints only after an arrow, Enter, or Tab key press, and keep them afterwards.
- [x] 2.2 Add a Vitest witness bound to `gui-shell/keyboard-hints-appear-after-first-keyboard-navigation`.

## 3. Verify

- [x] 3.1 Run the full gui Vitest suite, vue-tsc, the relevant Playwright specs, and `make spec-check`.
- [x] 3.2 Capture after screenshots of the status bar before and after keyboard use.
