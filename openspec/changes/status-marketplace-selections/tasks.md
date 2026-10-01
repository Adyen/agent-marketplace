## 1. Regressions first

- [x] 1.1 Add status command tests for the synced Marketplace pin, the organization collection without an organization lock, and the unverified-artifact pin; confirm they fail against the original behavior.
- [x] 1.2 Add a doctor command test proving a prerequisite satisfied only by a Marketplace selection produces no unmet warning.

## 2. Implementation

- [x] 2.1 Select Marketplace artifacts from the verified cache in inspect mode, merging required-pin health errors and wrapping the transient-organization resolver.
- [x] 2.2 Report a pin with cached revision metadata but no verified artifact as a hard error naming its resource coordinate and the `agent-marketplace sync` remedy; keep never-acquired pins soft.
- [x] 2.3 Set the new option in status and doctor only; keep list, deinit, remove cleanup, sync, and the TUI unchanged.
- [x] 2.4 Bind every scenario with test annotations and run the focused tests, validation, and spec checks.

## 3. Delivery

- [x] 3.1 Review and commit the implementation locally without pushing.
- [ ] 3.2 `openspec archive status-marketplace-selections --yes`
