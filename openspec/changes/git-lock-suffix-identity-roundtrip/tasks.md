# Tasks

## 1. Regression witnesses

- [x] 1.1 Add v1/v2 repeated-suffix round-trip, mutation-validation, and ordinary-byte tests in `internal/lockfile`; verify the round-trip tests fail before implementation with `go test ./internal/lockfile -run 'Test(RepeatedSuffix|ChangedNormalizedGitURL|OrdinaryGitURL)' -count=1`.
- [x] 1.2 Add an isolated local-bare-repository command test for initial sync, repeated-suffix URL serialization, targeted `lock --upgrade`, and locked/check sync while retaining a second source; verify it fails before implementation with `go test ./cmd/agent-marketplace -run '^TestSyncRepeatedGitSuffixLockIdentitySurvivesUpgrade$' -count=1`.
- [x] 1.3 Add a focused missing-fetch-mapping fallback test in `cmd/agent-marketplace`; verify it fails before implementation with `go test ./cmd/agent-marketplace -run '^TestNewFetchURLResolverPreservesRepeatedSuffixFallback$' -count=1`.
- [x] 1.4 Add a GC reachability test for a decoded repeated-suffix lock and verify it fails before implementation with `go test ./internal/cache -run '^TestGarbageCollectRetainsRepeatedSuffixLockIdentity$' -count=1`.

## 2. Identity preservation

- [x] 2.1 Add the private canonical suffix identity, `FetchURL`, `NewGitRegistry`, and clone-only wire encoding; verify v1/v2 codec round trips and existing YAML goldens with `go test ./internal/lockfile`.
- [x] 2.2 Use `NewGitRegistry` for acquired Git and plugin records, construct targeted selectors from canonical URLs and compare them directly, and retain authored URLs only for fetch transport; verify the isolated lifecycle test plus lock-selection tests.
- [x] 2.3 Normalize GC lock documents once and use their normalized registry URLs for reachability; verify the repeated-suffix GC test and existing GC package tests.

## 3. Focused delivery checks

- [x] 3.1 Run `go test ./internal/lockfile ./internal/gitidentity ./internal/cache`.
- [x] 3.2 Run the focused `cmd/agent-marketplace` lifecycle and fetch-fallback tests.
- [x] 3.3 Run `make spec-check`.

## 4. Review

- [ ] 4.1 Obtain lead approval that the current delta is ready to archive; verify no further scope corrections are requested.

## Workflow follow-up

- Archive `git-lock-suffix-identity-roundtrip` after review authorization; do not archive as part of this implementation handoff.
