---
status: accepted
---

# Marketplace indexes Git and mirrors canonical resource artifacts

Git repositories remain the publishing source of truth, while a Marketplace indexes approved sources for discovery and may distribute immutable, content-addressed resource artifacts. Each approved source explicitly uses index-only or mirror acquisition, with mirror as the operator default; clients never silently fall back between modes. This hybrid model preserves publisher workflows and provenance while giving public and private Marketplace operators a reliable distribution path that does not require clients to clone every source repository.

## Considered options

- A metadata-only index was rejected because it does not provide reliable or efficient distribution.
- An upload-native package registry was rejected because it would replace Git as the publishing workflow and create a second source of truth.
- Repository snapshots were rejected as the mirror unit because resources must remain independently installable across every selectable kind.

## Consequences

Mirroring requires a stable canonical artifact schema, digest verification, redistribution-license policy, durable metadata and blob storage, and a stronger threat model than direct Git discovery. Index-only mode remains available for sources that cannot legally or practically be mirrored.
