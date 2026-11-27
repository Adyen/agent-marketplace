# Tasks

## 1. Exact Place API response

- [x] 1.1 Add exact, eligible Place prerequisite serialization and API tests for historical digests, explicit empty, unknown, invalid declarations, and unchanged ordinary responses; verify with `go test ./pkg/marketplace/api -run 'Test(ExactPlace|ResourceDetail)' -count=1`.
- [x] 1.2 Update the canonical OpenAPI `ResourceRevision.requires` description and scoped `ResourceRequirement.kind` schema; verify the OpenAPI contract tests pass.

## 2. Acquisition boundaries

- [x] 2.1 Require indexed prerequisites before online Place archive acquisition and before cache-only archive reads; test missing/null refusal and explicit-empty acceptance with `go test ./internal/marketplace -run 'Test(AcquirePlacePackage|ReadLockedPlacePackage|FetchLockedPlacePackage)' -count=1`.
- [x] 2.2 Mark only known-empty Place fixtures with `requires: []`, preserving unknown fixtures; verify the affected package tests pass.

## 3. Hosted integration and gates

- [x] 3.1 Preserve the real SQLite/fsblob hosted fixture, exact `Requires` publication check, and online/cache-only/deselection order assertions; verify with `go test ./cmd/agent-marketplace -run '^TestHostedPlacePrerequisitesOrderPublicationAndDeselection$' -count=1 -v`.
- [x] 3.2 Run `make validate TEST_PACKAGES='./internal/marketplace ./pkg/marketplace/api ./cmd/agent-marketplace'`, `make spec-check`, and `make validate-e2e`; record each result.

## 4. Archive

- [ ] 4.1 After lead authorization, run `openspec archive deliver-exact-place-prerequisites --yes` and verify the archived canonical deltas with `make spec-check`.
