## Implementation

- [x] Add `internal/updatecheck`: version classification, latest-release lookup, 24-hour cache and notice throttle, with table and `httptest` tests bound to every scenario.
- [x] Wire the CLI: suppression rules, concurrent refresh bounded to one second after the command, one `info:` line on terminal standard error.
- [x] Show the notice as a bounded TUI notification without delaying the first frame.
- [x] Keep command-test and E2E sandboxes offline by setting `AGENT_MARKETPLACE_NO_UPDATE_CHECK`.
- [x] Document the notice and opt-out in `README.md`, `site/docs/install.md`, and the environment reference.
- [x] Verify scoped tests, `make validate`, `make spec-check`, and `make tui-qa`.
- [x] Adversarially review the change and archive it.
