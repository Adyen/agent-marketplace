# Tasks

## 1. Tests first

- [x] 1.1 Add failing regressions annotated `// spec: plugin-delivery/<slug>` for each scenario of the `External catalog entries deliver from their locked commit` requirement, and verify they fail on the base for the missing delivery

## 2. Delivery of external catalog entries

- [x] 2.1 Resolve each selected external catalog entry to its locked registry record and exact snapshot, decode its package with the lock-time catalog policy, and verify the decomposition, subdirectory, and moved-ref regressions pass
- [x] 2.2 Deliver the committed post-exclusion member keys through the existing delivery resolution and package generations, and verify the exclusion and `sync --check` regressions pass
- [x] 2.3 Block only the affected instance when the locked record or snapshot is unavailable, and verify the failure regression passes with other resources delivered
- [x] 2.4 Update existing regressions and the `plugin.delivery.pending` row in `pkg/harness-compilation/diag/codes.md` that asserted pending delivery for external entries, and verify the focused pipeline and command packages pass

## 3. Integration

- [ ] 3.1 Run `make validate`, `make spec-check`, and `make validate-e2e` and verify they pass
- [x] 3.2 Run `openspec archive plugin-external-entry-delivery --yes`
