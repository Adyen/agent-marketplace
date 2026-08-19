# Freeze Marketplace protocols and contracts

## What
Introduce the frozen public protocol surface for the agent-marketplace
serve/discovery epic:

- `api/openapi.yaml` — canonical OpenAPI contract.
- `pkg/marketplace/api` — Go constants and wire types shared with the server,
  CLI, and generated clients.
- `pkg/marketplace/analyze` — stable security-analyzer finding codes.
- Consumer-side config fields for `marketplaces:` and `allowed_marketplaces:`.
- Canonical artifact schema and collection manifest in
  `pkg/harness-compilation/artifact`.

## Why
Later implementation beads (`pf99.2` through `pf99.8`) need a stable contract to
build against. Freezing the protocol first prevents drift between the server,
consumer config, CLI, and artifact format.

## Impact
- Adds new Go packages and an OpenAPI contract.
- Extends `internal/config` with additive `marketplaces` fields (no new
  `apiVersion`; v2 remains canonical after `hl0u`).
- Does not enable a public Marketplace in generated user config.
