# Configuration Inventory Metrics

## Status

Approved design for measuring registry and resource adoption across the Agent Marketplace userbase. This design extends detailed telemetry while preserving the basic-only community-report contract.

## Goal

Provide a Grafana dashboard that can:

- rank the approximately 50 most-adopted registries and resources;
- display every observed registry and resource without a fixed top-k limit;
- filter adoption by resource kind, registry, configuration scope, harness, and installation ID;
- reconstruct the latest reported effective configuration at the dashboard's selected time; and
- show configuration changes over time for one installation.

The dashboard measures reporting installations, not people. `installation.id` remains the resettable pseudonymous identity described by the telemetry contract.

## Non-goals

- Collect inventory at the basic telemetry level.
- Send inventory through the community-report route.
- Identify users, repositories, workstations, or local filesystem paths.
- Treat a configuration that has not run Agent Marketplace as current.
- Export resource contents, descriptions, tags, prompts, terminal data, or registry credentials.
- Provision a Grafana dashboard from this repository; the repository supplies the metric contract and reference queries for the operations-owned dashboard.

## Decision

Represent each successfully resolved effective configuration with three OpenTelemetry integer gauges, plus one bounded omission counter:

| OTLP instrument | Prometheus-compatible name | Meaning |
|---|---|---|
| `agent_marketplace.config.snapshot` | `agent_marketplace_config_snapshot` | One complete observed configuration generation; value is its observation ordering key |
| `agent_marketplace.config.registry` | `agent_marketplace_config_registry` | One registry in that generation; value `1` |
| `agent_marketplace.config.resource` | `agent_marketplace_config_resource` | One effectively enabled resource in that generation; value `1` |
| `agent_marketplace.config.inventory_omissions` | `agent_marketplace_config_inventory_omissions` | Count of whole inventory snapshots omitted, labeled by bounded reason |

The snapshot gauge value is a Unix-microsecond observation ordering key. If two
observations in one CLI runtime share or regress against its previous wall-clock
value, the runtime advances the later value logically. This provides a value
that queries can compare directly while making cross-process ties negligible
without telemetry-owned durable state; registry and resource gauge values remain
`1`.

Exact registry and resource dimensions are detailed telemetry. All four
inventory instruments are exported only to the primary route when the effective
level is `detailed`.

The existing detailed trace spans remain useful for diagnostics and are not the source queried by the adoption dashboard.

## Configuration generation

A stable `config.fingerprint` associates all registry and resource points with one complete effective configuration. It is the lowercase hexadecimal SHA-256 digest of a canonical telemetry-safe representation containing:

- effective harnesses and their source;
- sanitized configured registries and their telemetry-visible fields; and
- effectively resolved resources and their telemetry-visible fields.

Canonicalization sorts harnesses, registries, and resources by all represented fields before hashing. Identical telemetry-visible configurations produce the same fingerprint regardless of map or resolver iteration order. A configuration that changes and later returns to an earlier state reuses the earlier fingerprint; query time determines which observation is relevant.

A random snapshot ID is deliberately excluded. Random generations would create a new time series on every invocation. A stable fingerprint creates new series only when the represented configuration changes.

## Metric dimensions

`installation.id`, `telemetry.schema.version`, `cli.version`, `os.name`, and `arch` remain OpenTelemetry resource attributes. They are not duplicated as instrument attributes.

### Snapshot

| Attribute | Value |
|---|---|
| `config.fingerprint` | Canonical configuration digest |
| `harnesses` | Sorted comma-separated effective harnesses |
| `harness.scope` | `organization`, `user`, `repo`, `local`, or `unknown` |
| `registry.count` | Number of represented configured registries |
| `resource.count` | Number of represented resolved resources |

### Registry

| Attribute | Value |
|---|---|
| `config.fingerprint` | Snapshot fingerprint |
| `registry.id` | Canonical aggregation identity |
| `registry.location` | Sanitized display location |
| `registry.path` | Registry subpath for remote registries only |
| `registry.ref` | Configured ref |
| `config.scope` | Defining configuration layer |
| `registry.required` | Boolean string |
| `registry.default` | Boolean string |

### Resource

| Attribute | Value |
|---|---|
| `config.fingerprint` | Snapshot fingerprint |
| `registry.id` | Canonical registry aggregation identity |
| `registry.location` | Sanitized display location |
| `registry.path` | Registry subpath for remote registries only |
| `registry.ref` | Configured ref |
| `resource.kind` | Skill, command, subagent, hook, or MCP kind |
| `resource.name` | Exact resolved resource name |
| `config.scope` | Effective source layer |
| `resource.required` | Boolean string |

### Inventory omission

| Attribute | Value |
|---|---|
| `reason` | `inventory_too_large`, `invalid_inventory`, or `queue_full` |

Counts are strings because the existing telemetry wrapper accepts string attributes. They remain bounded summary dimensions on the snapshot instrument. Omission reasons are a bounded enum and carry no registry, resource, fingerprint, or installation-level instrument attribute.

## Registry identity and redaction

Registry normalization must aggregate equivalent Git transports while retaining a useful display value:

- trim surrounding whitespace;
- remove URL user information, query strings, and fragments;
- lowercase the hostname;
- normalize SCP-style and URL-style Git locations to the same host/path identity;
- remove a trailing `.git` and trailing slash from the identity;
- retain the configured registry subpath as the separate `registry.path` dimension; and
- represent every local path or `file://` registry as `local`, without a path-derived identifier.

For example, these locations share one `registry.id`:

```text
git@github.com:Adyen/example.git
https://github.com/Adyen/example.git
https://github.com/adyen/example/
```

Their identity is `github.com/adyen/example`. The display location remains sanitized but does not need to collapse transport syntax.

No credential, query, fragment, or local filesystem component may enter a metric attribute or the fingerprint input.

## Emission boundary

The shared `pipeline.Resolve` result is the authoritative inventory source. Emit inventory after catalog loading and resource resolution have completed and only when there are no hard resolution errors. Soft warnings do not prevent emission.

This boundary covers the TUI and configured-registry CLI flows that use the shared pipeline. It avoids separate instrumentation in `list`, `status`, `sync`, `install`, `remove`, and `update` command handlers. Ad-hoc `--from` browsing that is not part of effective configuration is excluded. A mutating flow must report its post-mutation resolution; intermediate failed states are not complete snapshots.

Telemetry remains best effort. Fingerprinting or recording failure must not alter command output, filesystem behavior, or exit status.

Recording enqueues each complete generation on a bounded queue for prompt
background export, so network I/O never runs on the resolving caller's
goroutine. The inventory provider uses delta temporality and forces a flush
after every queued observation. This is required for long-lived TUI and other
sessions: multiple observations of the same gauge series must be exported
promptly rather than collapsed or delayed until the normal periodic reader
interval. When the queue is full, the generation is omitted and
`agent_marketplace.config.inventory_omissions{reason="queue_full"}` is
incremented.

## Route isolation

The telemetry runtime uses instrument-family and route-specific providers so detailed inventory cannot leak through a shared reader to the optional project-report route:

- the primary command provider owns the basic command counter and duration histogram;
- the primary inventory provider owns all four detailed inventory instruments, uses delta temporality, and supports prompt per-observation export;
- the project-report provider owns only the basic command counter and duration histogram;
- command recording writes to both command providers when both routes are active;
- inventory gauge and omission recording writes only to the primary inventory provider; and
- when project reporting is folded into the primary route, no separate project-report provider is needed.

Each provider must own its exporter and reader. Exporter cache reuse must not attach one SDK exporter instance to multiple readers. Shutdown flushes every provider within the existing bounded timeout. Queue envelopes continue to preserve route and signal.

Instrument-specific attribute allowlists remain separate. Inventory attributes must not be added to the command metric allowlist.

The optional project-report route exports only the basic command instruments. Tests must prove inventory instruments never reach that route, including when detailed primary telemetry and project reporting are simultaneously enabled.

## Point-in-time query model

Prometheus-compatible storage retains old registry and resource series after configuration changes. Dashboard queries must first select the fingerprint attached to the greatest snapshot gauge value for every installation at the evaluation time, then retain only items with that fingerprint.

This requires the collector/backend to promote OTLP resource attributes to
stable Prometheus labels. At minimum, `installation.id` must become
`installation_id`; schema, CLI, OS, and architecture resource attributes may be
promoted when dashboards use them. Point attributes such as
`config.fingerprint` must likewise have stable names such as
`config_fingerprint`. Stock OpenTelemetry components do not universally perform
resource-attribute promotion, so the production collector configuration and
stored labels must be verified.

Define the latest generation over an operator-selected freshness window:

```promql
topk by (installation_id) (
  1,
  last_over_time(agent_marketplace_config_snapshot[$inventory_lookback])
)
```

A current resource set is conceptually:

```promql
last_over_time(agent_marketplace_config_resource[$inventory_lookback])
and on (installation_id, config_fingerprint)
topk by (installation_id) (
  1,
  last_over_time(agent_marketplace_config_snapshot[$inventory_lookback])
)
```

Use the corresponding registry metric for registry adoption. The production dashboard should use recording rules for these repeated expressions if evaluation cost warrants them.

An instant query evaluated at the Grafana dashboard's `to` time reconstructs the latest reported configuration at or before that point, bounded by `$inventory_lookback`. A range query evaluates the same selection at each step and supports state timelines. Installations with no observation inside the lookback are stale and intentionally absent from adoption counts.

## Grafana views

### Adoption overview

- Reporting installations inside the lookback.
- Unique registry identities.
- Unique resource identities.
- Registry and resource counts per installation.
- Snapshot freshness and CLI-version distribution.

### Top and complete tables

Top resources:

```promql
topk(50, count by (resource_kind, resource_name, registry_id) (<current resources>))
```

All resources use the same expression without `topk`. Registry tables group by `registry_id`, `registry_location`, and `registry_path`. Grafana table panels use instant queries, labels-to-fields/time-series-table transforms, sortable columns, and client-side column filters, matching the existing Fluffy deployment-overview pattern.

### Installation drill-down

A restricted dashboard variable selects `installation_id`. Panels then show:

- latest observation time and freshness;
- effective harnesses and harness source;
- configured registries;
- enabled resources grouped by kind and scope; and
- configuration fingerprint changes as a state timeline.

Moving the dashboard time range reconstructs the installation's latest reported configuration at the selected endpoint. Access to installation-level detail follows the detailed telemetry access policy.

## Cardinality budget

Inventory metrics intentionally revise the earlier rule that exact resource and registry identifiers never appear as metric dimensions. The change is restricted to detailed primary telemetry and requires an operations-approved series budget before rollout.

Approximate active series are:

```text
installations × (1 + average registries + average enabled resources + observed omission reasons)
```

Observed omission reasons are bounded to at most three. Historical series also multiply by distinct configuration fingerprints and CLI resource-attribute versions retained by the backend. Before enabling production ingestion, measure:

- reporting installations;
- average, p95, and maximum registries and resources per snapshot;
- configuration-fingerprint churn per installation;
- total active series and daily series creation; and
- dashboard query latency for top-k, complete-table, and installation filters.

Client-side safeguards allow at most 1,000 registries, 10,000 resources, and
512 UTF-8 bytes per string attribute. Exceeding any bound increments
`agent_marketplace.config.inventory_omissions{reason="inventory_too_large"}` and
omits the whole snapshot rather than emitting an incomplete generation. Invalid
construction uses `invalid_inventory`, and a full prompt-export queue uses
`queue_full`.

Measured benchmarks establish implementation cost for specific fixtures, not
production limits or latency guarantees. `BenchmarkRecordInventory`, with 10
registries, 100 resources, detailed telemetry, and a local OTLP HTTP endpoint,
measured approximately 0.60–0.61 ms/op, 1.186 MB/op, and 5,264–5,267 allocs/op.
`BenchmarkResolveInventoryWithLocalDiskAndHTTPExport`, resolving 50 selected
skills from one local registry and waiting for each snapshot to reach a local
OTLP HTTP endpoint, measured a median 3.155 ms/op, 1,714,779 B/op, and 12,592
allocs/op. Synchronizing each export prevents queue saturation from measuring
the omission path. Local registry disk I/O and local HTTP export are included;
remote registry access and materialization are excluded.

## Schema and documentation

This is an additive detailed schema change. The telemetry schema is major `1`, minor `1`. Update:

- telemetry preview output with representative snapshot, registry, and resource gauges plus the omission counter;
- telemetry disclosure and retention documentation;
- the transparent telemetry design's detailed-metric exception;
- benchmarks and documented overhead; and
- external operations guidance with collector cardinality and Grafana query prerequisites.

## Verification

Tests must cover:

1. Stable fingerprints across input orderings.
2. Fingerprint changes for every represented field.
3. URL normalization equivalence and credential/query/fragment/local-path redaction.
4. Exact organization, user, repo, and local source attribution.
5. Effectively resolved resources after exclusions and required-resource rules.
6. No inventory emission at `off` or `basic` levels.
7. Inventory export to the detailed primary route.
8. No inventory export to the community route in standalone and dual-route configurations.
9. Complete post-resolution emission from TUI and representative non-TUI commands.
10. No emission for hard-error or oversized partial snapshots.
11. Queueing and shutdown behavior for the additional primary metric provider.
12. Preview and disclosure output.

Because inventory construction runs on the `Resolve` path, retain a realistic benchmark for canonicalization and recording and compare `Resolve` before and after integration. Benchmark results include allocations and are reported with the implementation.

## Implementation sequence

1. Add failing metric-contract, routing-isolation, normalization, fingerprint, and shared-pipeline tests plus the required benchmark.
2. Split primary and community metric providers and add typed inventory gauge recording.
3. Build canonical telemetry-safe inventory from `ResolveResult` and record it at the shared successful-resolution boundary.
4. Update preview, disclosure, retention, runbook, and cardinality guidance.
5. Run scoped tests and benchmarks, then `make test`, `make ci-lint`, and `make build`.
