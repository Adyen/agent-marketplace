## 1. Tests

- [x] 1.1 Add resolver regression test for an optional entry ordered before a required entry (`internal/resolver/resolver_test.go`)
- [x] 1.2 Add TUI regression test for a layered optional-then-required miss (`internal/tui/missing_selection_test.go`)
- [x] 1.3 Confirm both fail before the fix

## 2. Implementation

- [x] 2.1 Store the reported not-found error per key and upgrade it to required in `buildOccurrences`
- [x] 2.2 Set the TUI row's soft flag to `soft && !required`

## 3. Verification

- [x] 3.1 Run resolver, TUI, and pipeline package tests and `make spec-check`
- [x] 3.2 Run `make ci-lint`
