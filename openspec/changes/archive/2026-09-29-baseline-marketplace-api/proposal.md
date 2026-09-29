## Why

`marketplace-protocols` was brought under scenario-to-test traceability by minimal correction only: OpenSpec refuses to drop scenarios from a modified requirement, so two scenario titles no longer match their corrected content, and most requirements still name Go identifiers (`DefaultCapabilities()`, `ToIR`/`FromIR`, `ValidateArchive`, `ArchiveLimits`, package paths) that the spec rules forbid. The capability taxonomy adopted with spec authority names its successor `marketplace-api`, and the legacy documents mapped to it are still unharvested.

## What Changes

- Removes the `marketplace-protocols` capability; `openspec/specs/marketplace-protocols/` is deleted once the archive applies the removal.
- Adds a `marketplace-api` capability describing the current observable Marketplace protocol surface only: OpenAPI paths, schemas, and media types; the capabilities document; the wire vocabulary of selectable resource kinds; the analyzer finding-code catalog; consumer Marketplace endpoint registration and installation pins (`marketplaces`, `allowed_marketplaces`) and their validation and layer merge; and the artifact and collection-manifest wire formats.
- Retitles the two drift-corrected scenarios so each title matches its content, and carries every existing scenario behavior across under the new capability with the same annotated tests.
- Adds scenarios for three observable behaviors already proven by existing passing tests but not yet bound to a scenario: the fixed public media types on inspection content routes, the acquisition modes and server features declared by the capabilities document, and the optional browser route templates of the well-known document.
- Absorbs ledger rows 2.3, 2.7, 2.9, and 4.7 of the legacy-doc harvest: `docs/adr/0002-separate-marketplace-identity-from-installed-names.md`, `docs/adr/0005-use-content-addressed-collection-manifests-and-selection-edges.md`, `docs/adr/0007-require-immutable-inspection-projections.md`, and `docs/plans/2026-08-18-marketplace-serve-design.md` are deleted; every claim from them that is not carried into the spec is listed under **Discarded claims** in `design.md`.
- Moves every `// spec: marketplace-protocols/...` test annotation to the new scenario identities.
- **No behavior changes**: no runtime code, OpenAPI document, or test logic changes; only spec prose, annotation comment lines, the harvest ledger, and deleted legacy documents.

## Capabilities

### New Capabilities

- `marketplace-api`: the public Marketplace protocol surface shared by the server, CLI, and generated clients — OpenAPI contract, capabilities and well-known documents, wire kind vocabulary, analyzer finding-code catalog, consumer Marketplace configuration, and artifact and collection-manifest wire formats.

### Modified Capabilities

- `marketplace-protocols`: every requirement is removed; the capability is replaced by `marketplace-api`.

## Impact

- Documentation and specs only; no runtime behavior changes.
- Test files in `pkg/marketplace/api`, `pkg/marketplace/analyze`, `pkg/harness-compilation/artifact`, and `internal/config` change in comment lines only (annotation moves and three added annotations on existing passing tests).
- `openspec/changes/harvest-legacy-docs/tasks.md` checks off rows 2.3, 2.7, 2.9, and 4.7.
- Two reference links in `docs/plans/2026-09-07-unified-resource-config-design.md` that point at deleted ADRs are removed.
