# Design: baseline-marketplace-api

## Context

`marketplace-protocols` was bound to tests by minimal correction only, so two scenario titles no longer match their corrected content and the requirements still name Go identifiers and a legacy schema version. The capability taxonomy adopted with spec authority names `marketplace-api` as this surface's successor, and the harvest ledger still holds four legacy documents mapped to it. This baseline rebaselines the capability in wire-observable wording, carries every proven scenario across, and absorbs those four documents.

## Goals / Non-Goals

**Goals:**
- Replace `marketplace-protocols` with `marketplace-api` describing only current observable Marketplace behavior, with every scenario proven by an annotated passing test.
- Absorb harvest rows 2.3, 2.7, 2.9, and 4.7: delete the four legacy documents, record every non-carried claim under **Discarded claims**.
- Zero behavior changes: no runtime code, OpenAPI document, or test-logic edits.

**Non-Goals:**
- Baselining any adjacent surface (marketplace-store indexing/publication, marketplace-web rendering, marketplace-operations deployment, resource-selection and config-mutation install flows). Claims owned by those surfaces are discarded, not speced here.
- Absorbing harvest rows 4.19, 4.21, and 4.22 (admin previews, OIDC admin auth, operator-exposed resources): their content targets surfaces this baseline does not carry and stays for the marketplace-web, marketplace-operations, and marketplace-store baselines.

## Decisions

### D1. Replace, do not modify

OpenSpec cannot rename a capability in place, so the delta REMOVES all six `marketplace-protocols` requirements and ADDs the rebaselined requirements under `marketplace-api`. Alternative — MODIFIED requirements in place — rejected: it would keep the drifted titles and the forbidden identifier wording, and the taxonomy already names the successor.

### D2. Scenario carry and the two retitles

All 16 existing scenario behaviors carry across with the same annotated tests; test files change in comment lines only. The two drift-corrected scenarios are retitled so each title matches its content: *Required paths and schemas are present* becomes *OpenAPI document declares the Marketplace paths and schemas*, and *Archive digest resolves the self-reference problem* becomes *Archive digest verification catches archive tampering* (the scenario states verification outcomes, not the placeholder mechanism).

Three scenarios are added for observable behaviors already proven by existing passing tests that were never bound to a scenario. Each is proven by adding one `// spec:` comment line to those tests; no new test logic is introduced:

- *Inspection content routes serve the public media types* (inspection projection media-type set declared by the OpenAPI document).
- *Capabilities document declares acquisition modes and server features* (index-only and mirror acquisition, the catalog projection feature).
- *Well-known document exposes browser templates only when configured* (browser templates appear only with a browser base, and stay optional in the OpenAPI schema).

### D3. Wire-observable wording

Requirement and scenario text is reworded to observable surfaces with no Go identifiers and no schema-version history:

| Old wording | New wording |
|---|---|
| `DefaultCapabilities()` / `APIVersions` / `ArtifactSchemaVersions` | the capabilities document declares API major versions and artifact schema versions |
| `SupportedResourceKinds` / `IsMarketplaceKind` | the selectable resource kind vocabulary; exactly those values are Marketplace-selectable |
| `api.ToIR` / `api.FromIR` / `ir.Kind*` | Marketplace wire kind values converted to and from the harness-neutral resource representation |
| `FindingCodes()` / `pkg/marketplace/analyze/codes.md` | the finding-code catalog / the Markdown catalog shipped beside the analyzer |
| `ValidateMarketplaceEntries` / `MarketplaceURLBlockedError` | `marketplaces` entries are validated / URLs are reported as blocked, listing the blocked URLs |
| `marketplaces` in the legacy v2/v3 schema | `marketplaces` (configuration field, no version history) |
| `CanonicalArtifact` / `Encode` / `Decode` / `FromIR` | a resource artifact is built / encoding produces / decoding restores |
| `VerifyContentDigest` / `VerifyArchiveDigest` / `ValidateArchive` | content-digest, archive-digest, and archive validation pass/fail |
| `ArchiveLimits` | configured archive limits |
| `CollectionManifest` / `EncodeCollectionManifest` / `VerifyCollectionManifest` | a collection manifest is built / encoding produces / verification recomputes |

### D4. Annotation slug map

Every annotation moves to `marketplace-api/<slug>`; the slug is the scenario title lowercased with each run of non-alphanumeric characters replaced by one hyphen:

| Old slug | New slug |
|---|---|
| `required-paths-and-schemas-are-present` | `openapi-document-declares-the-marketplace-paths-and-schemas` |
| `go-constants-match-the-openapi-enum` | `capabilities-document-matches-the-openapi-contract` |
| `selectable-resource-kinds` | `selectable-resource-kinds` |
| `bidirectional-kind-conversion` | `bidirectional-kind-conversion` |
| `code-catalog-parity` | `code-catalog-parity` |
| `marketplace-entries-are-validated` | `marketplace-entries-are-validated` |
| `marketplace-url-allowlist` | `marketplace-url-allowlist` |
| `layered-config-wiring` | `layered-config-wiring` |
| `canonical-artifact-round-trip` | `canonical-artifact-round-trip` |
| `payload-contains-resource-name` | `payload-contains-resource-name` |
| `security-rejection-during-decode` | `security-rejection-during-decode` |
| `oversized-resource-rejection` | `oversized-resource-rejection` |
| `archive-digest-resolves-the-self-reference-problem` | `archive-digest-verification-catches-archive-tampering` |
| `collection-manifest-round-trip` | `collection-manifest-round-trip` |
| `deterministic-ordering` | `deterministic-ordering` |
| `content-digest-excludes-the-digest-field` | `content-digest-excludes-the-digest-field` |

New annotations go on existing passing tests:

| New slug | Test |
|---|---|
| `inspection-content-routes-serve-the-public-media-types` | `TestOpenAPIMediaTypesMatchInspectionContract` |
| `capabilities-document-declares-acquisition-modes-and-server-features` | `TestDefaultCapabilitiesListsAcquisitionModes`, `TestDefaultCapabilitiesAdvertisesCatalogProjectionV1Alpha1` |
| `well-known-document-exposes-browser-templates-only-when-configured` | `TestDefaultWellKnownAdvertisesBrowserTemplatesOnlyWhenConfigured`, `TestOpenAPIBrowserRoutesAreNuxtHTMLAndWellKnownTemplatesOptional` |

### D5. Archive cleanup

`openspec archive baseline-marketplace-api --yes` applies the removal and creation. If it leaves an empty `openspec/specs/marketplace-protocols/` directory, it is deleted in the same pull request (the bootstrap change set this precedent for `embedded-harness-compilation`).

## Risks / Trade-offs

- [A scenario title and its annotation slug drift apart] → `specaudit trace` fails on both dangling annotations and unproven scenarios; `make spec-check` runs before handoff and in CI.
- [A carried scenario is weakened during rewording] → acceptance review compares each new scenario against the removed requirement verbatim; the slug map above fixes the 1:1 carry.
- [Deleting the four legacy documents removes leads other baselines expected] → code and tests remain the authority for those baselines, and **Discarded claims** records every non-carried claim with the surface that owns it.
- [The archive leaves an empty spec directory] → verify and delete per D5 in the same pull request.

## Migration Plan

One pull request: the change artifacts (this commit), then the annotation moves, ledger check-offs, and document deletions, then `openspec archive baseline-marketplace-api --yes` as the final commit. Rollback is a revert of the pull request; no runtime behavior changes at any step.

## Discarded claims

Every claim from the four absorbed documents that is not carried into `marketplace-api`, with its reason. "Owned by `<capability>`" means current code implements it (or it is planned) and the named capability's own baseline derives it from code, which is the authority; it is deliberately not speced here.

### `docs/adr/0002-separate-marketplace-identity-from-installed-names.md`

- Registry ID immutability across repository moves, mutable canonical registry slugs with permanent prior-slug aliases, and registry origin history: owned by `marketplace-store` (registry lifecycle); no annotated scenario in this capability proves it. The wire presence of the `Registry` schema and registry paths is carried by the OpenAPI contract scenario.
- The registry-local name is the default installed name, and installed-name collisions stop installation and require an explicit alias: owned by `resource-selection`/`config-mutation` (consumer install flow); proven by CLI tests outside this capability's annotated set.
- Human-facing commands may use a local Marketplace alias resolved before portable configuration is written: owned by `config-mutation`; the carried validation scenarios prove only the field-level validation and merge of `marketplaces` entries.
- Endpoint-registration and installation-pin record fields beyond the carried validation scenarios (alias uniqueness and reserved-alias rules, provisional instance identity, unsafe identity/endpoint rejection): implemented and tested but proven by tests not bound to the carried scenario set; they enter a spec when their owning consumer-configuration scenarios are baselined.

### `docs/adr/0005-use-content-addressed-collection-manifests-and-selection-edges.md`

- A collection revision preserves exact direct includes and a deduplicated expansion of exact non-collection member revisions: the manifest encoding, digest, and ordering contract is carried; expansion and deduplication semantics are owned by `resource-selection`/`marketplace-store` (collection expansion), proven by tests outside this capability's annotated set.
- Installation persists both the collection pin and its leaf pins, each recording whether it is selected directly or by one or more exact collection revisions; removing one selector cannot remove a pin still selected elsewhere: owned by `resource-selection`/`config-mutation` (installation selectors).
- Nested collections are expanded by the Marketplace; cycles and conflicting revisions of one coordinate are rejected; exact duplicates are deduplicated while retaining inclusion paths: owned by `marketplace-store`/collection expansion.
- Mirror and index-only leaves may coexist in one collection revision: owned by `marketplace-store` (acquisition mixing).
- Install and update acquire and verify the complete new leaf set before atomically changing configuration: owned by `config-mutation` (transactional install).

### `docs/adr/0007-require-immutable-inspection-projections.md`

- Every published resource revision, including index-only revisions, has a bounded immutable inspection projection: carried only as wire presence (inspection paths, `InspectionManifest`, `InspectionFile` schemas, and the inspection content routes' media types); the projection contract itself is owned by `marketplace-store` and proven by tests outside this capability's annotated set.
- Versioned resource assessments reference the inspection manifest and carry findings against its canonical paths and byte ranges: owned by `marketplace-store` (assessment surface); only schema presence (`ResourceAssessment`, `AssessmentFinding`) is carried.
- The inspection manifest lists the primary path and every file's canonical path, digest, media type, size, and inspection class; textual files and permitted raster images are fetched separately by digest: owned by `marketplace-store` (inspection projection schema and serving).
- Binary files expose identity without a body URL when rendering is prohibited; SVG is not rendered inline and remote images never load; rendered Markdown is sanitized with an escaped raw view: owned by `marketplace-web` (browser rendering policy).
- Registry-policy file-count, per-file, and artifact-size limits bound the complete projection, and publication fails rather than truncating it: owned by `marketplace-store`/`marketplace-operations` (operator limits).
- Projection bytes are immutable-cacheable while effective revocation is fetched and evaluated separately: owned by `marketplace-store` (cache and revocation control plane).

### `docs/plans/2026-08-18-marketplace-serve-design.md`

- Status header, promotion boundary, V1 acceptance boundary, implementation phases, validation strategy, risk table, and launch inputs: planning history, not behavior; no claim carried.
- The Marketplace objective and deployment topology (one policy domain per deployment, worker/API role split, network defaults, TLS at the proxy): owned by `marketplace-operations` (serve/deployment surface).
- The operator configuration schema (instance, storage, refresh, policy, registry entries, credentials, revocations) and the rule that secret values are forbidden in server configuration: owned by `marketplace-operations`.
- The storage and job model (SQLite/PostgreSQL, filesystem/S3 blob stores, durable jobs, atomic publication, backup unit, consistency checks): owned by `marketplace-store`.
- Registry domain model (Registry ID/slug/origin lifecycle, roots, duplicate coordinate resolution by root order, unlisting, retention) and the registry occurrence/revision/assessment identity model: owned by `marketplace-store`; only the wire presence of their schemas and routes is carried by the OpenAPI contract scenario.
- Indexing and publication steps (isolated acquisition, ingestion, canonicalization, analysis, license policy, atomic publish, stale retention): owned by `marketplace-store`.
- The artifact byte profile (gzip header fields, compression level, DEFLATE profile, tar ordering and metadata pinned by golden fixtures) and the rule that an encoder change requires a new artifact schema version: determinism and verification outcomes are carried; byte-profile pinning is proven by golden fixtures outside this capability's annotated tests and enters a spec with its owning artifact scenario.
- API versioning policy (additive within `/api/v1`, breaking in a new major) and immutable cache headers on artifact routes: server policy and serving behavior owned by `marketplace-operations`/`marketplace-store`.
- Search semantics (latest non-revoked revision per coordinate from listed registries, server-owned relevance), revocation blocking resolution without recalling cached bytes, and catalog projection details (pagination parameters, ETag validators, availability enum, registry schema privacy): owned by `marketplace-store` and the catalog surface; proven by tests not bound to the carried scenarios.
- The browser resource route as the stable human locator and the well-known document mapping it back to the API base: carried (paths and the well-known requirement); Nuxt-specific rendering is not.
- CLI behavior: federated search with per-endpoint errors, install consent and risk display, locator resolution, exact pinning, transactional collection install, sync/status/update semantics, cache and garbage collection, offline behavior, `--allow-revoked` override, and required-pin policy: owned by the CLI-facing baselines (`resource-selection`, `config-mutation`, `config-layering`, `acquisition-lock`); the consumer configuration validation and merge carried here is the field-level contract those flows share.
- Per-command enforcement breadth of `allowed_marketplaces` (every effective-config load) and `allowed_registries` for index-only acquisition: owned by `config-layering`/pipeline baselines; the allowlist's validation semantics are carried.
- The security model (trust semantics, analyzer categories and outcomes, "never a safe badge", CSP, log/label exclusions) and observability (metrics, readiness, logs): owned by `marketplace-operations`/`marketplace-store`/analyzer baselines; only the finding-code catalog contract is carried.
- The web frontend contract (Nuxt, generated TypeScript client, page list, accessibility budgets): owned by `marketplace-web`.
