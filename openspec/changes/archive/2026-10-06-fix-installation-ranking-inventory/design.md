## Context

See proposal.md for motivation. The metrics reader currently returns every fingerprint in a 30-day series window before Go selects latest inventories. The response cap is 8 MiB and the request deadline is two seconds. Snapshot values, not scrape timestamps, encode observation order in Unix microseconds. Production's original query returned 18,767,541 bytes; the proposed projection returned 3,547,475 bytes. These are separate live captures, not a controlled performance benchmark or proof of pod connectivity.

## Goals / Non-Goals

**Goals:** Preserve current adoption semantics while removing superseded generations from the backend response and preventing legitimate rules from invalidating selectable resource counts.

**Non-Goals:** New public fields, operator configuration, metrics limits, telemetry emission, frontend changes, production deployment changes, and server-side aggregate counting.

## Decisions

1. Keep the existing installationMetricsQuery(labels) interface. Let S be max by (installation_id, config_fingerprint) of last_over_time(snapshot with the configured selectors [30d]). Select L = S == on (installation_id) group_left max by (installation_id) (S). Do not use topk(1): it arbitrarily picks one of equal-time fingerprints and would erase ambiguity. Preserve every tied maximum so the existing Go selection can omit that installation.
2. Let R be max by (installation_id, config_fingerprint, registry_id, resource_kind, resource_name) of last_over_time(resource with the same configured selectors and resource_kind!="rule" [30d]). Return label_replace(L, "inventory_type", "snapshot", "", "") or label_replace((R and on (installation_id, config_fingerprint) L), "inventory_type", "resource", "", ""). Set matching preserves ties without arithmetic join cardinality errors. Retain Go's freshness, future-observation exclusion, deduplication, origin matching and validity horizon.
3. Validate rule rows as legitimate telemetry in aggregateInstallationMetrics, but skip them when accumulating adoption. Continue rejecting genuinely unknown kinds, missing identity fields, and invalid gauge values. Filtering rules in the query is not a substitute for defensive handling at the parser boundary.
4. Keep the body cap, deadline, redirect protection, aggregate-only cache and unavailable fallback unchanged. Raising the cap alone was rejected because it only postpones historical growth and leaves rule rejection. Fully backend-aggregated counts were rejected for this bounded patch because origin matching and distinct-installation deduplication across multiple origins already belong to Go.

## Verification

Write failing regressions before changing code: mixed valid rule and skill inventory must return skill adoption, while unknown kind and invalid rule gauge remain invalid; installation-ordered search with mixed inventory must expose the skill count. Independently assert the full deterministic query for escaped selector values, latest-snapshot value comparison, resource join and rule exclusion. Validate the query with the existing Prometheus-compatible HTTP API using self-contained synthetic vectors: a newer empty inventory removes old resources, duplicate inputs collapse, equally recent conflicting fingerprints remain ambiguous, rule inputs are excluded, and unrelated deployment selectors do not leak into results. Preserve the evaluator's frozen synthetic result and test it offline through the real installation metrics reader; it must count only `rA/skill/alpha` and exclude tied, stale, and future observations. The YAML input fixture is a replay/coherence artifact, not proof of evaluator execution. Keep default Go tests offline. Retain existing oversized-response and fallback controls. Run focused installation tests, one final make validate scoped to the API package, and make spec-check after archive in the implementation phase. Capture narrow existing adoption/search benchmark before/after only in implementation; no full-suite or E2E gate is required for unchanged CLI/frontend paths.

## Risks / Trade-offs

- Latest inventories can themselves outgrow the cap -> preserve safe fallback; future aggregation is a separate design.
- Repeating snapshot selection increases query complexity -> verify real evaluator semantics and keep the two-second deadline; live candidate capture took 1.55 seconds from the developer host, not the API pod.
- Rules could be confused with malformed unsupported kinds -> skip only validated rule observations and retain negative tests for unknown kinds.

## Migration Plan

Deploy the fixed API binary through the normal release and GitOps workflow. No configuration migration is needed. Rollback restores previous omission behavior. After deployment, require counts from the public search API and absence of refresh failures; production changes are not authorized by this implementation task.
