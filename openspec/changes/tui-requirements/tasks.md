## 1. Prove TUI prerequisite behavior

- [ ] 1.1 Add spec-annotated internal/tui tests for every delta scenario and observe them fail on the unchanged base.

## 2. Implement

- [ ] 2.1 Precompute per-row prerequisite state and render the Requires and Required by detail sections.
- [ ] 2.2 Render the `req` badge and unmet warning marker without changing unrelated rows.
- [ ] 2.3 Apply the prerequisite-aware toggle-off exclusion through the serialized mutation and report unmet warnings.
- [ ] 2.4 Report also-selected and would-also-select prerequisites after toggle on.

## 3. Verify and document

- [ ] 3.1 Add a deterministic tools/tui-qa journey for detail sections and toggle feedback.
- [ ] 3.2 Reconcile published TUI and prerequisite documentation.
- [ ] 3.3 Run `openspec archive tui-requirements --yes` in the same pull request as the implementation.
