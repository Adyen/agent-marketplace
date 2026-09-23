---
status: accepted
---

# Marketplace indexes Git and mirrors canonical resource artifacts

Git repositories remain the publishing source of truth, while a Marketplace indexes approved registries for discovery and may distribute immutable, content-addressed resource artifacts. Each registry explicitly uses index-only or mirror acquisition, with mirror as the operator default; clients never silently fall back between modes. This hybrid model preserves publisher workflows and provenance while giving public and private Marketplace operators a reliable distribution path that does not require clients to clone every registry repository.

## Considered options

- A metadata-only index was rejected because it does not provide reliable or efficient distribution.
- An upload-native package registry was rejected because it would replace Git as the publishing workflow and create a second source of truth.
- Repository snapshots were rejected as the mirror unit because resources must remain independently installable across every selectable kind.

## Consequences

Mirroring requires a stable canonical artifact schema, digest verification, redistribution-license policy, durable metadata and blob storage, and a stronger threat model than direct Git discovery. The v1 `archive_digest` remains the digest of the original final gzip bytes. Because standard-library gzip output may change across Go releases, publishers also retain the placeholder-form archive used to derive that digest. Clients can verify those exact proof bytes directly without recreating the final gzip stream; the lock schema and legacy artifact endpoint remain unchanged. Index-only mode remains available for registries that cannot legally or practically be mirrored.
