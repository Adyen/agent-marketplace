## 1. Source form

- [x] 1.1 Add failing component and browser tests for field-label consistency, inline errors and clearing, hint timing, and content-width alignment.
- [x] 1.2 Add a shared input table exercised by the client validator and canonical CLI Git parser; verify invalid input disables addition without rejecting CLI-accepted locations.
- [x] 1.3 Implement the shared form validation, field-error wiring, and styles; verify the focused tests pass and update the source-form design guidance.

## 2. Integration

- [x] 2.1 Rebuild embedded frontend assets and pass GUI typechecking, Vitest, sources and accessibility Playwright tests, local API tests, make spec-check, and make validate.
- [x] 2.2 Capture matched light and dark before/after source-form screenshots from the mocked preview, with artifacts retained outside the repository.

## Workflow follow-up

- Run `openspec archive polish-gui-source-form --yes` as the final commit of the pull request and verify the archived result with `make spec-check`.
