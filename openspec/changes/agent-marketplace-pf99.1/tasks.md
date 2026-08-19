# Tasks: agent-marketplace-pf99.1

## API protocol surface
- [x] `pkg/marketplace/api/kinds.go` — marketplace resource kind constants.
- [x] `pkg/marketplace/api/types.go` — wire types (`Capabilities`, `WellKnown`,
  `ResourceListItem`).
- [x] `pkg/marketplace/api/capabilities.go` — version and mode constants, plus
  `DefaultCapabilities()` / `DefaultWellKnown()`.
- [x] `pkg/marketplace/api/capabilities_test.go` — kind/enum sanity tests.
- [ ] `pkg/marketplace/api/openapi_test.go` — load `api/openapi.yaml` and
  assert routes/schemas (pending successful YAML parse path).
- [x] `api/openapi.yaml` — v1 OpenAPI contract with all required paths and
  schemas.

## Analyzer codes
- [x] `pkg/marketplace/analyze/codes.go` — stable finding codes and `Finding`
  type.
- [x] `pkg/marketplace/analyze/codes.md` — catalog documentation.
- [x] `pkg/marketplace/analyze/codes_test.go` — catalog parity test.

## Consumer config (next slice)
- [ ] `internal/config/marketplace.go` — `MarketplaceEntry`,
  `MarketplaceResourcePin`, validation helpers.
- [ ] `internal/config/allowed_marketplaces.go` — `ValidateMarketplaceURLs`.
- [ ] Wire `marketplaces`/`allowed_marketplaces` into `Config`,
  `LocalConfig`, `v3.go`, `merge.go`, `effective_config.go`.
- [ ] `internal/config/marketplace_test.go` and bench.

## Canonical artifact (next slice)
- [ ] `pkg/harness-compilation/artifact/canonical.go` — encode/decode/verify.
- [ ] `pkg/harness-compilation/artifact/collection.go` — collection manifest.
- [ ] Tests, benchmarks, and golden fixtures.

## Verification
- [ ] `make test`
- [ ] `make lint`
- [ ] `make build`
