## 1. Tests first

- [ ] 1.1 Command tests for `list` bound to the new and modified `resource-listing` scenarios, failing on the base commit
- [ ] 1.2 Command tests for `status` bound to the new and modified `status-reporting` scenarios, failing on the base commit

## 2. Implementation

- [ ] 2.1 Shared pipeline predicate for missing Marketplace cache evidence; inspection skips the dry run when it holds
- [ ] 2.2 `list` reports unknown sync state with the sync remedy instead of an out-of-sync count
- [ ] 2.3 `status` DRIFT reports unknown with the Marketplace cache evidence and sync remedy

## 3. Verification

- [ ] 3.1 Focused command and pipeline tests, `make spec-check`, `make validate`
