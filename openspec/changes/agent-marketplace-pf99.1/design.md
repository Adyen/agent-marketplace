# Design: Marketplace protocols and contracts

## Dependencies
- `agent-marketplace-hl0u` (v2 canonical config) must land first; this bead
  adds fields to the v2 schema without introducing a new `apiVersion`.

## Decisions
- OpenAPI is the source of truth; the Nuxt/TS client generates from it.
- Go constants in `pkg/marketplace/api` are the runtime source of truth and are
  tested to match the OpenAPI enums.
- Analyzer codes follow the `pkg/harness-compilation/diag` pattern with a
  Markdown catalog enforced by a parity test.
- Consumer config mirrors the existing `allowed_registries` pattern for
  `allowed_marketplaces`.
- Artifact schema v1 is a deterministic `tar.gz` with a `manifest.json` and a
  canonical `resource/<kind>.json` payload.
- Collection manifests are standalone JSON, not tar archives, because collections
  are virtual.

## Files
- `api/openapi.yaml`
- `pkg/marketplace/api/{kinds,types,capabilities}.go`
- `pkg/marketplace/api/{capabilities,openapi}_test.go`
- `pkg/marketplace/analyze/{codes.go,codes.md,codes_test.go}`
- `internal/config/{marketplace.go,allowed_marketplaces.go,marketplace_test.go,marketplace_bench_test.go}`
- `pkg/harness-compilation/artifact/{canonical.go,collection.go}` + tests and
  golden fixtures.
