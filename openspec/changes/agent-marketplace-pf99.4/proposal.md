# Marketplace bounded indexing worker

## What

Implement the Marketplace operator-side indexing worker that ingests registries from Git,
builds resource artifacts, and publishes them atomically under configurable policy and
limits:

- `pkg/marketplace/config` — standalone operator configuration loader (separate from
  the CLI user config) with a v1 schema for marketplace identity, storage backends,
  refresh policy, acquisition limits, license allowlists, registry definitions, and
  revocations.
- `pkg/marketplace/acquire` — isolated bounded Git acquisition with credential
  plumbing, askpass isolation, ref validation, and strict repository/file/byte limits.
- `pkg/marketplace/ingest` — registry ingestion from cloned trees: catalog loading,
  artifact builds, license/policy enforcement, and registry-revision manifest construction.
- `pkg/marketplace/worker` — bounded scheduler/worker with `Refresh`, `Run`, and
  `Reindex`, durable leased jobs, and context-aware subprocess cancellation.
- `cmd/agent-marketplace/serve.go` — `serve` and `serve reindex` commands that wire the
  worker into the CLI.
- Supporting extensions to `analyze/options.go`, `artifact/builder.go`,
  `artifact/revision.go`, and `internal/cache/registry.go` for license propagation,
  canonical registry-revision payloads, and Git URL/ref validation reuse.

## Why

pf99.3 provided the persistence layer and durable jobs; pf99.4 adds the end-to-end
operator worker that turns registry configuration into published Marketplace revisions.
The worker must be bounded and safe: it runs arbitrary `git` processes, fetches from
configured origins, and writes to the store, so isolation, validation, cancellation,
and policy enforcement are first-class design concerns.

## Impact

- New `pkg/marketplace/config`, `acquire`, `ingest`, and `worker` packages.
- `agent-marketplace serve` and `agent-marketplace serve reindex` become runnable.
- License metadata flows from operator config/registry definitions through to resource
  revisions and allowlist rejection findings.
- Git acquisition reuses shared URL/ref validation from `internal/cache` but runs in
  isolated temporary workspaces with credential helpers.
