## Context

See proposal.md. `installationMetricsQuery(labels)` returns latest snapshots and their resource rows; `aggregateInstallationMetrics` applies freshness, ambiguity, origin matching, hosted isolation and deduplication in Go. Production on 2026-10-19: 593 latest snapshots and 26,236 resource rows, 9,311,245 bytes, 2.8–3.1 s from a developer host, against the 8,388,608-byte budget and a 3 s production timeout. 47% of the rows belong to `registry_id="local"`, which never counts. A hand-built prototype of the aggregate below returned 484 rows, 73,020 bytes, in 1.65 s against the same Prometheus. A global value check over every latest resource row alone took 2.2 s, so the value check is limited to contributing rows (approved contract edit).

## Goals / Non-Goals

**Goals:** Move adoption evaluation into PromQL so the response is bounded by counted coordinates; keep every existing adoption rule and the malformed-data fallback; prove the query in a real engine.

**Non-Goals:** Changing the 8 MiB budget, timeouts, refresh scheduling or outcome labels; telemetry emission; public API fields; VictoriaMetrics compatibility; rollout.

## Decisions

### 1. Query plan from registry metadata

`newInstallationQueryPlan(registries map[store.RegistryID]store.Registry, marketplaceID store.MarketplaceID) installationQueryPlan` moves the existing owner/identity logic out of `aggregateInstallationMetrics`:

- `Hosted []store.RegistryID`: every registry ID in `registries`, sorted.
- `Identities []struct{ Identity string; Registry store.RegistryID }`: each sanitized non-local origin identity (`registryid.SanitizeRegistryTelemetry`) owned by exactly one registry, sorted by identity.
- `KnownRegistries map[store.RegistryID]bool`: unchanged semantics (true for registries with a non-local identity, false when any of their identities is ambiguous).
- `MarketplaceID`.

`installationMetricsQuery(labels map[string]string, plan installationQueryPlan) string` builds the query; `fetch` passes the plan built from the registries it already receives. Strings are quoted with `strconv.Quote`; regex alternations quote each member with `regexp.QuoteMeta` (PromQL regexes are fully anchored RE2). Sorting keeps the query deterministic.

### 2. Query shape

`SEL` is the existing sorted, quoted selector list. `W` is `[30d]`.

```
S  = max by (installation_id, config_fingerprint) (last_over_time(agent_marketplace_config_snapshot{SEL}W))
L  = (S == on (installation_id) group_left max by (installation_id) (S))
F  = ((L unless on (installation_id) (count by (installation_id) (L) > 1)) >= (time() - 2592000) * 1e6 <= time() * 1e6)
H  = label_replace(max by (installation_id, config_fingerprint, registry_id, resource_kind, resource_name) (last_over_time(agent_marketplace_config_resource{SEL,resource_kind!="rule",source_kind="marketplace",marketplace_id="<mp>",registry_id=~"<hosted alternation>"}W)), "am_registry", "$1", "registry_id", "(.*)")
G0 = max by (installation_id, config_fingerprint, registry_id, resource_kind, resource_name) (last_over_time(agent_marketplace_config_resource{SEL,resource_kind!="rule",source_kind="",marketplace_id="",registry_id=~"<identity alternation>"}W))
G  = label_replace(... label_replace(G0, "am_registry", "<registry 1>", "registry_id", "<QuoteMeta identity 1>") ..., "am_registry", "<registry n>", "registry_id", "<QuoteMeta identity n>")
P  = (H or G)                      // omit H or G when its list is empty; omit P and C when both are
C  = count by (am_registry, resource_kind, resource_name) (max by (installation_id, am_registry, resource_kind, resource_name) (P and on (installation_id, config_fingerprint) F))
I  = invalid markers (decision 3)
U  = suspicious hosted identities (decision 3)

query = label_replace(C, "inventory_type", "count", "", "")
     or label_replace(count(F), "inventory_type", "fresh", "", "")
     or label_replace(min(F), "inventory_type", "valid_until", "", "")
     or I or U
```

Freshness and ambiguity are evaluated against the snapshot value (Unix microseconds) at the request's `time`, matching the Go rules: keep `cutoff <= observed <= now`, drop installations with more than one fingerprint at their maximum. `max by (installation_id, am_registry, …)` collapses Git and hosted observations of one coordinate and multiple origins of one registry before counting distinct installations. Hosted IDs are path-free segments and Git identities contain a host path, so `H or G` cannot drop a distinct observation.

### 3. Malformed data

Exact checks become marker rows with `inventory_type="invalid"`; any marker makes the result invalid. Every marker drops identity labels with `count(...)` so no Installation ID is returned:

- snapshot values in `L` that are `<= 0`, `> 9007199254740992`, or `!= floor(L)`;
- latest resource rows (raw resource series `and on (installation_id, config_fingerprint) L`) selected by label matchers: `source_kind!~"|marketplace"`; `source_kind="",marketplace_id!=""`; `resource_kind!~"skill|command|subagent|hook|mcpserver|collection|rule"`; `registry_id=""`; `resource_name=""`;
- snapshot or resource series in the window with `installation_id=""` or `config_fingerprint=""`;
- contributing rows `P != 1`.

Hosted identity validity (`validHostedInventoryIdentity`: Unicode whitespace, control characters, path separators, dot components, 512-byte bound) is not exactly expressible in RE2, so the query returns a superset: latest hosted resource rows whose `marketplace_id`, `registry_id` or `resource_name` matches `|.*[^!-~].*|.{513,}|\.|\.\.|.*[/\\].*`, aggregated `max by (source_kind, marketplace_id, registry_id, resource_kind, resource_name)` with `inventory_type="suspicious"`. Go validates each with the existing rules; a failure is invalid, a pass is ignored.

### 4. Response parsing

`aggregateInstallationMetrics(samples, plan, now)` accepts only these `inventory_type` values:

- `count`: non-empty `am_registry`, `resource_kind`, `resource_name`; value a positive integer at most 2^53. Rows for a registry absent from `registries` are ignored. A counted registry becomes known.
- `fresh`: exactly one when present; positive integer. Absent means no fresh installation: `errInstallationMetricsUnavailable`.
- `valid_until`: exactly one when `fresh` is present; integer within `[cutoff, now]` in microseconds; `ValidUntil = UnixMicro(value) + 30 days`.
- `invalid`: any row returns `errInvalidInstallationMetrics`.
- `suspicious`: validated as above.

Anything else is invalid. Response size, timeout, redirect, credential, caching and scheduling code is unchanged.

### 5. Real-engine proof

`TestInstallationMetricsQueryPromtool` in `pkg/marketplace/api` skips unless `AM_PROMTOOL_IMAGE` is set, mirroring `tools/marketplace-proxy`. It builds the query through `installationMetricsQuery` with a test plan, writes a promtool unit-test file (`rule_files: []`, `input_series`, `promql_expr_test` with `eval_time` and `exp_samples`) generated from a Go table, and runs `docker run --rm -v <dir>:/work --entrypoint promtool $AM_PROMTOOL_IMAGE test rules /work/<file>`. Because the expression is generated, the fixture cannot drift from production code. Cases: latest-only and removed resources, stale and future observations, scrape freshness not substituting for observation freshness, tied fingerprints, Git plus hosted dedup, multiple origins of one registry, ambiguous identity excluded, foreign Marketplace and unknown registry excluded, local and rule excluded, many installations producing one row, no fresh installation producing no `fresh` row, each invalid marker, suspicious superset including a legitimate non-ASCII name, and absence of `installation_id` and `config_fingerprint` in every expected sample.

`make installation-query-check` runs that test with `AM_PROMTOOL_IMAGE=$(PROMETHEUS_IMAGE)` (`PROMETHEUS_IMAGE ?= prom/prometheus:v3.5.0`) through `tools/witness-check -require`, so a skip or non-execution fails. `tools/ci-plan` gains an `installation_query` output for `pkg/marketplace/api/installation_*` changes, and `ci.yml` gains an `Installation query` job that runs the target when it is selected.

Go unit tests replace per-installation canned responses with canned aggregate responses: parsing and expiry, every inventory type, invalid and suspicious rows, absent `fresh`, plan construction (hosted list, unambiguous identities, ambiguous exclusion, local exclusion), deterministic query text, and the existing search, refresh, oversized-response and fallback paths.

## Risks / Trade-offs

- Query length grows with registries and origins (about 100 bytes per identity). Prometheus accepts POST bodies of this size; VictoriaMetrics' default query-length limit is out of scope.
- A malformed value on an observation that cannot contribute no longer forces the fallback. Such rows cannot change counts; the approved contract accepts this to stay within the latency budget.
- Engine-level semantics are proven only in the Docker gate; root tests skip it like the proxy gate, and CI runs it whenever installation sources change.

## Migration Plan

Release the API binary and bump the production image through the normal GitOps workflow (outside this change). No configuration migration. After deployment, require `installation_count` in public search and `outcome="success"` refreshes. Rollback restores the previous query, which fails on current production volume.
