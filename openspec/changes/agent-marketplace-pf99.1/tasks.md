# Tasks: agent-marketplace-pf99.1

## API protocol surface
- [x] `pkg/marketplace/api/kinds.go` — marketplace resource kind constants.
- [x] `pkg/marketplace/api/types.go` — wire types (`Capabilities`, `WellKnown`,
  `ResourceListItem`).
- [x] `pkg/marketplace/api/capabilities.go` — version and mode constants, plus
  `DefaultCapabilities()` / `DefaultWellKnown()`.
- [x] `pkg/marketplace/api/capabilities_test.go` — kind/enum sanity tests.
- [x] `pkg/marketplace/api/openapi_test.go` — load `api/openapi.yaml` and
  assert routes/schemas.
- [x] `api/openapi.yaml` — v1 OpenAPI contract with all required paths and
  schemas.

## Analyzer codes
- [x] `pkg/marketplace/analyze/codes.go` — stable finding codes and `Finding`
  type.
- [x] `pkg/marketplace/analyze/codes.md` — catalog documentation.
- [x] `pkg/marketplace/analyze/codes_test.go` — catalog parity test.

## Consumer config
- [x] `internal/config/marketplace.go` — `MarketplaceEntry`,
  `MarketplaceResourcePin`, validation helpers.
- [x] `internal/config/allowed_marketplaces.go` — `ValidateMarketplaceURLs`.
- [x] Wire `marketplaces`/`allowed_marketplaces` into `Config`,
  `LocalConfig`, `v3.go`, `merge.go`, `effective_config.go`.
- [x] `internal/config/marketplace_test.go` and bench.

## Canonical artifact
- [x] `pkg/harness-compilation/artifact/canonical.go` — encode/decode/verify.
- [x] `pkg/harness-compilation/artifact/collection.go` — collection manifest.
- [x] `pkg/harness-compilation/artifact/canonical_test.go` — round-trip,
  determinism, payload, rejection, and digest tests.
- [x] `pkg/harness-compilation/artifact/collection_test.go` — collection
  round-trip, determinism, and validation tests.
- [x] `pkg/harness-compilation/artifact/canonical_bench_test.go` — benchmarks
  for `Encode`, `Decode`, `FromIR`, and `VerifyArchiveDigest`.

## OpenSpec documentation
- [x] `openspec/changes/agent-marketplace-pf99.1/.openspec.yaml`,
  `proposal.md`, `design.md`, `tasks.md`.
- [x] `openspec/specs/marketplace-protocols/spec.md` — frozen protocol
  requirements.

## Verification
- [x] `make test`
- [x] `make lint`
- [x] `make build`
