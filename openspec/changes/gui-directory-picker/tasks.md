## 1. Native desktop binding

- [x] 1.1 Add a fake-dialog Go witness for an existing Location, home fallback for blank/nonexistent/file locations, returned directory, and cancellation. Bind `gui-configuration/the-dialog-starts-at-an-existing-location-or-home`.
- [x] 1.2 Implement the Wails binding and the testable dialog seam without changing local HTTP endpoints.

## 2. Shared source form

- [x] 2.1 Add Vitest coverage for keyboard-accessible Browse, selection through the typed Location edit path, cancellation, and absence for Git, Marketplace and missing binding. Bind `gui-configuration/browse-is-absent-outside-native-directory-source-forms` and `gui-configuration/cancelling-the-dialog-preserves-location`.
- [x] 2.2 Extend `gui/tests/sources.spec.ts` with a stub for the Wails picker and the real local API fixture: choose a folder, assert Location and no config write yet, then submit Add source and assert the expected entry. Bind `gui-configuration/choosing-a-folder-fills-location-without-adding-a-source`.
- [x] 2.3 Implement Browse beside Location using the existing form controls and update the source form section of `gui/DESIGN.md` for this layout. Rebuild the committed desktop frontend bundle.

## 3. Verify and archive

- [x] 3.1 Run GUI typecheck, Vitest, built sources Playwright tests and the desktop Go package tests with native coverage. Capture matched light and dark before/after form screenshots; label any stubbed dialog clearly.
- [ ] 3.2 Run `make validate` and check spec traceability. Run `make spec-check` on the final archived tree.
- [ ] 3.3 Run `openspec archive gui-directory-picker --yes` for the final commit of the same pull request.
