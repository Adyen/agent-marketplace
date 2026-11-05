# Tasks

## 1. Absent organization lock guidance

- [x] 1.1 Add command tests that run `sync --locked`, `sync --offline`, and `lock --check` with a valid organization configuration and no organization lock, asserting `v4.marketplace-lock-required`, a hint naming online `agent-marketplace sync` or `sync --check`, no administrator or organization-file advice, no Marketplace requests, and unchanged organization files, locks, and state; verify they fail on the base
- [x] 1.2 Add regression tests asserting the administrator hint for present organization locks that are malformed, stale, wrong-scope, untrusted, or mismatched; verify they pass
- [x] 1.3 Mark the absent-lock refusal and select the new hint for organization scope only; verify the tests from 1.1 and 1.2 pass with `make test TEST_PACKAGES="./internal/remedy ./internal/config ./cmd/agent-marketplace" TEST_FLAGS="-count=1"`
- [x] 1.4 Confirm the TUI never reports this code for an absent organization lock (it loads online and acquires transiently) and record the evidence; verify no `internal/tui` change is needed
- [x] 1.5 Archive the change during the parent-owned final PR step
