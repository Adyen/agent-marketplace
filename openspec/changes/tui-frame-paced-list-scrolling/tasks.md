## 1. Ordered input and presentation

- [x] 1.1 Add annotated regression tests for ordered wheel reversals at both list boundaries, wrapped rows, modifiers, and ignored events; observe the intended missing-behavior failure before implementation.
- [x] 1.2 Add resource-list-only frame pacing with one trailing repaint; verify controlled-clock tests for final selection and bounded view construction pass.
- [x] 1.3 Preserve immediate keys, resize, snapshot and pane updates with executable commands and stale-timer rejection; verify annotated state-transition tests pass.

## 2. Runtime integration and evidence

- [x] 2.1 Integrate pacing into the actual TUI program and preserve the final model's sanitized sync-and-quit error identity; verify program reachability, error, interruption, editor and terminal-guard regression tests pass.
- [x] 2.2 Leave non-list wheel handling unchanged; verify preview, tree, overlays, palette and undersized-terminal controls pass.
- [x] 2.3 Retain the parent-authored full-path wheel benchmark and compare unpaced and integrated input with complete event counts and independently expected selection; preserve labeled before/after evidence outside the repository.
- [x] 2.4 Add a real-terminal encoded wheel-burst and reversal journey; verify the displayed resource details through `make tui-qa-check` and `make tui-qa` without machine-dependent timing assertions.

## 3. Published contract and delivery

- [x] 3.1 Update existing TUI reference and product contract, including cell-motion reporting and frame-paced list presentation; reconcile published claims with the complete relevant source.
- [ ] 3.2 Verify the committed candidate with scoped TUI and CLI consumer tests, native validation, spec checks and Marketplace TUI E2E checks; retain complete logs and resolve actionable review findings.
- [ ] 3.3 Run `openspec archive tui-frame-paced-list-scrolling --yes` in the same pull request and verify the synchronized main specs and archived scenario bindings.
