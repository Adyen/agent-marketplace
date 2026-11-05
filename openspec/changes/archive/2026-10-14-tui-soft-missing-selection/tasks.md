## 1. Tests

- [x] 1.1 Add TUI tests for optional missing selections keeping the assessment and for unverifiable selections still failing it (`internal/tui/missing_selection_test.go`)
- [x] 1.2 Confirm the optional-missing-selection test fails before the fix

## 2. Implementation

- [x] 2.1 Add `UnavailableSoft` to the TUI row and set it for soft not-found candidates in `appendUnavailableSelectionRows`
- [x] 2.2 Make `rowsHaveUnavailable` ignore soft rows

## 3. Verification

- [x] 3.1 Run the TUI package tests and `make spec-check`
- [x] 3.2 Run `make ci-lint`
