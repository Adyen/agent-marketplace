## 1. Publication Indexing

- [ ] 1.1 Add the nullable per-revision prerequisite column as new SQLite and Postgres migrations, with no backfill; verify migration tests on fresh and existing databases.
- [ ] 1.2 Record prerequisites during ingest with the shared catalog parser, in declaration order, with credential-redacted hints, and keep unindexed revisions distinct; verify that re-publication fills an unindexed revision and that different indexed prerequisites conflict.
- [ ] 1.3 Add tests annotated for every marketplace-publication scenario.

## 2. Resource Detail API

- [ ] 2.1 Add shared source-root derivation and path-hint matching helpers used by both the Marketplace server and the client; verify unit cases for equality, file-prefix rejection, nested-root ambiguity, unknown source path, and unsafe hints.
- [ ] 2.2 Add `requires` (with `resolved`) and `required_by` to latest resource detail using the approved queries, visibility predicate, scope rule, and completeness order; verify SQLite and Postgres store tests and server tests, including all negative visibility and completeness controls.
- [ ] 2.3 Declare the fields in `api/openapi.yaml`, regenerate the web API types, and prove that the client decodes the new response; verify the OpenAPI contract test and `node scripts/generate-api.mjs --check`.
- [ ] 2.4 Add tests annotated for every marketplace-api scenario.

## 3. Acquisition-Lock Requires Edges

- [ ] 3.1 Accept, validate, and canonicalize requires edges in lock `selected_by`; verify byte-identical collection-only locks, requires-edge round-trips, and every invalid-document case.
- [ ] 3.2 Admit resources reachable through requires edges, and keep requires provenance separate from collection selections; verify that collection consumers never see requires edges and that legacy configuration stays collection-only.
- [ ] 3.3 Make every lock rewrite keep valid requires edges, drop stale ones, prune records only they held, and never add a requires edge; verify install, update, remove, and sync rewrites.
- [ ] 3.4 Add tests annotated for every acquisition-lock-codec scenario.

## 4. Resolution and Lookup

- [ ] 4.1 Resolve requires-held pins as implied resources (expansion enabled) or as preview entries (disabled), matched through their edges; verify enabled, disabled, and orphaned cases.
- [ ] 4.2 Add configured Marketplace lookup with binding-based hints, an exact registry-commit occurrence with verified manifest membership and no current-revision fallback, current-revision lookup with exact-occurrence roots, the path rule, exclusions, and no offline requests; verify each reason with a fake Marketplace client.
- [ ] 4.3 Preview unrecorded candidates without affecting materialization, and report `unrecorded` when expansion is enabled; verify that configuration and lock bytes stay unchanged.
- [ ] 4.4 Add tests annotated for every new or modified resource-prerequisites scenario.

## 5. Removal

- [ ] 5.1 Allow removal of a still-required Marketplace prerequisite held by requires edges: remove its lock record and edges, write the exact exclusion only when expansion is enabled, and report dependents; verify enabled, advisory, and non-collection-message cases.
- [ ] 5.2 Add tests annotated for every resource-removal scenario.

## 6. Documentation and Archive

- [ ] 6.1 Update the site reference pages for prerequisites, the Marketplace API, and the acquisition-lock format with the release-N boundary and the deferred recording behavior.
- [ ] 6.2 Run `go run ./tools/specaudit trace` and `openspec validate --all --strict`.
- [ ] 6.3 Run `openspec archive marketplace-requirements --yes`.
