## 1. Regression and implementation

- [x] 1.1 Add annotated keyboard and refresh regressions for every query-local folding scenario and observe the fold regression fail before implementation.
- [x] 1.2 Capture the existing fuzzy-ranked view benchmark before the fix, add query-local fold state and first-header refresh anchoring, and verify the focused regression passes.
- [x] 1.3 Run the full TUI package tests and capture the same benchmark after the fix to check fold, ranking, and query lifecycle compatibility.

## 2. Integration

- [x] 2.1 Verify the integrated change with make validate, make spec-check, strict OpenSpec validation, make tui-qa-check, and make tui-qa.
- [x] 2.2 Run openspec archive fix-tui-search-folding --yes in the same pull request and verify the synced tui-navigation specification and archived artifacts.
