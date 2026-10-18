## 1. Tests first

- [ ] 1.1 Add command tests in `cmd/agent-marketplace` that drive the TUI dependency lifecycle in update mode for every new TUI scenario, each bound with a `// spec:` line, and observe them fail on the base

## 2. TUI update guard

- [ ] 2.1 Enable the existing selection guard for the TUI update lifecycle only

## 3. Verification

- [ ] 3.1 Focused TUI lifecycle tests, `make validate`, `make spec-check`, and `make tui-qa` pass
