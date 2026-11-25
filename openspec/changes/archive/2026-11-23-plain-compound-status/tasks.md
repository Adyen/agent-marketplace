## 1. Spec

- [x] 1.1 Add `status-reporting` requirement "Compound status reads in plain words" with its scenario.

## 2. Implementation and witnesses

- [x] 2.1 Reword compound status labels in `internal/session/rows.go`.
- [x] 2.2 Update Go, GUI, TUI QA, preview, and site tests and copy that quote the old strings.
- [x] 2.3 Bind the scenario to `TestRowStatusDecision` with `// spec: status-reporting/compound-status-reads-in-plain-words`.

## 3. Verify

- [x] 3.1 Run `make test` for session, tui, and localapi, the full gui vitest, vue-tsc, `make tui-qa-check`, the affected TUI QA journeys, and `make spec-check`.
- [x] 3.2 Archive the change.
