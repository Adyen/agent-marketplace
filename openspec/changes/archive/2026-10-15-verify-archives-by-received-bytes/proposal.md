## Why

`archive_digest` is the SHA-256 of the placeholder-form canonical archive. Every
verifier today decodes the final archive and re-compresses it with the running
binary's `compress/gzip` before comparing. Go 1.27 changed `compress/flate`
output, so an artifact or lock recorded by a Go 1.26 build fails verification in
a Go 1.27 Marketplace or client, and the reverse. The server's artifact route
has the same dependency and returns 500 for foreign-encoded archives.

## What Changes

- The Marketplace stores the placeholder-form archive alongside the final
  archive under a placeholder blob key. Its bytes hash to the archive digest,
  so they are verified by hash, not by re-encoding.
- New route `GET /api/v1/placeholder-archives/sha256/{hex}` serves the
  placeholder archive under the same ownership, revocation, and assessment gates
  as the artifact route. A missing placeholder is derived from the stored final
  archive only when re-encoding reproduces the digest.
- The existing artifact route serves stored final archives verified against the
  placeholder content; it never re-compresses when a placeholder is present.
- Publication reuses the stored archive digest when the newly built artifact is
  equal to the stored one ignoring its archive digest, so republishing identical
  content causes no digest churn across server toolchains.
- A background backfill derives missing placeholder archives for stored
  revisions; unreproducible ones are reported, not fatal.

No lock format, API route, existing response field, or digest value changes.

## Impact

- Affected specs: `marketplace-api` (new requirement), `marketplace-publication`
  (new requirements).
- Affected code: `pkg/harness-compilation/artifact`, `pkg/marketplace/{api,store,
  artifact,ingest,worker}`, `cmd/agent-marketplace/serve.go`, `api/openapi.yaml`.
- Clients fetch the placeholder archive and verify it by hash, fall back to the
  artifact route only on `404`, refetch stale cached copies, and report an
  encoding-mismatch diagnostic with remediation when neither form verifies.
  Index-only reconstruction checks the registry revision manifest by hash and
  matches rebuilt resources by content and support digests
  (`internal/marketplace`).
