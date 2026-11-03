# Tasks

## 1. Regression proof

- [x] 1.1 Add the `expired_final_attempt_lease_does_not_coalesce` conformance case with a `// spec: marketplace-registry-observation/an-expired-lease-on-the-final-permitted-attempt-does-not-coalesce-a-refresh-request` binding; verify it fails on the review head `6437198ab` because the request coalesces onto the dead job

## 2. Fix

- [x] 2.1 Make `JobActive` treat a leased job as active only while attempts remain or its lease is valid, and pass the current time from both `RequestRefresh` call sites; verify the regression case passes on SQLite and PostgreSQL
- [x] 2.2 Capture the applied migration count after seeding in the observation migration tests instead of hardcoding 10; verify both backends pass and appending a migration no longer breaks them

## 3. Integration

- [x] 3.1 Run `make spec-check`, `make validate`, and the witnessed PostgreSQL conformance and migration run with `POSTGRES_DSN` set; verify all pass with no PostgreSQL skip
- [ ] 3.2 Run `openspec archive fix-expired-final-attempt-coalescing --yes` as the final commit and verify `make spec-check` still passes
