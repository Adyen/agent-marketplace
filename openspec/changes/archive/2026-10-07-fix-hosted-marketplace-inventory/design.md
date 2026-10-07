## Context

See proposal.md for motivation. Resolved Marketplace resources already carry instance ID, registry ID and canonical resource name separately from their installed alias. Inventory currently sanitizes RegistryURL for every resource, including mirror acquisition whose RegistryURL is an endpoint with no repository path. The collector strips unknown attributes; the API projects registry IDs before matching them to Git origins.

## Goals / Non-Goals

**Goals:** Complete mixed inventories, explicit hosted identity, unchanged legacy Git behavior, isolated Marketplace counting and per-installation deduplication across acquisition paths.

**Non-Goals:** Production rollout, networking changes, acquisition or ownership changes, new public search fields, materialization verification, unique-user counting, and retroactive reconstruction of omitted reports.

## Decisions

1. Add optional `SourceKind` and `MarketplaceID` to inventory registries and resources, with `json:",omitempty"` so legacy-only fingerprint inputs retain their existing representation. Hosted observations carry source.kind=`marketplace`, marketplace.id=the verified instance ID, registry.id=the registry ID and resource.name=the canonical resource name. Legacy observations omit both new attributes. Keep schema major 1 and advance minor to 2.
2. Branch on IsMarketplace before Git sanitization. Validate the instance ID, registry ID and canonical name with the existing Marketplace segment validator and the inventory UTF-8/512-byte bounds. Hosted registry.location is the bounded literal `marketplace`, registry.path is empty, and resources retain their bounded resolved ref. Add one hosted registry row per instance/registry/config-scope/required tuple, with empty ref/path and default=false. Apply the existing total registry cap after hosted rows are added. Do not mutate resolved artifacts or use their endpoint/installed alias as telemetry identity.
3. Add source.kind and marketplace.id only to registry/resource attribute allowlists. Validate their paired presence and bounds at recording as well as construction. Keep inventory export detailed-primary-only, snapshot ordering, whole-generation rejection and existing omission counters.
4. Keep installationMetricsQuery(labels) unchanged externally. Its resource `max by` projection becomes exactly `(installation_id, config_fingerprint, source_kind, marketplace_id, registry_id, resource_kind, resource_name)`; all latest-snapshot matching, tied maxima, selector escaping and rule exclusion stay unchanged. Pass the serving Marketplace ID explicitly to the reader and aggregation. Untyped observations with no marketplace.id keep Git-origin matching; hosted observations match the serving instance and a known registry directly. Unknown source kinds or incoherent identity attributes are invalid. Valid reports for another instance are ignored. Deduplicate all accepted observations with the existing Installation ID plus resource coordinate key. An accepted hosted report makes that registry's count known, even when its Git origin cannot be matched.
5. Do not infer hosted identity from artifact Git provenance: it is not always available on metadata-only paths and leaves origin ambiguity. Do not weaken Git URL validation, append a fabricated endpoint path, or silently drop hosted resources from complete snapshots.
6. Collector support is additive: retain both attributes on registry/resource points, accept old points lacking both, require source.kind=marketplace when present, and require a nonempty bounded string marketplace.id for hosted points. Reject incoherent pairs and control-bearing IDs. No trace attributes, receivers, endpoints, networking, images or unrelated policy change.

## Verification

Write red regressions for mixed Git/hosted snapshot construction, root and pathful endpoints, mirror and index-only coordinates, aliases, identity bounds, fingerprint separation and source immutability. Exercise actual shared resolution plus OTLP export using isolated local fixtures. Test aggregation and public search for own/foreign instances, different registries, Git-plus-hosted deduplication, removal through a newer empty generation, and malformed-pair fallback. Pin the projected query independently. Run focused package checks and one final scoped make validate plus make spec-check. Exercise the author-owned collector config with the cached deployed collector version, including valid legacy/hosted and malformed points, and render its Kustomize overlay. Benchmark the modified inventory hot path with the same fixture before and after.

## Risks / Trade-offs

- Old collectors strip hosted identity and old APIs interpret registry IDs as Git identities: deploy readers and collector before new writers.
- Data omitted by old clients cannot be recovered from counters: require a fresh successful resolution after upgrading.
- An incomplete or malformed identity must not become a partial complete snapshot: preserve whole-generation omission.
- Public OTLP data remains untrusted telemetry rather than proof of installation: retain limits and unknown fallback.

## Migration Plan

Publish the additive collector support and API reader, then release the CLI. Validate a fresh hosted resource observation through the collector and public search after rollout. Rollback clients first; an older reader/collector must not be used as evidence that hosted counts are valid. Implementation performs only isolated local checks, not a production deployment.
