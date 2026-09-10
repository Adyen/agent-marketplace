# Telemetry

`agent-marketplace` can send telemetry about command usage and health over
vendor-neutral OTLP/HTTP. Telemetry is opt-in for unmanaged installations and
can be locked by organization policy for managed installations.

## Telemetry levels

| Level | Signals | When to use |
|---|---|---|
| `off` | None | No telemetry is collected or sent. |
| `basic` | Metrics | Command counts and durations with bounded, non-identifying attributes. |
| `detailed` | Metrics + traces | Basic metrics, primary-route configuration inventory gauges and omission counter, and lifecycle spans for commands, TUI, and pipeline stages. |
| `diagnostic` | Metrics + traces + events | A per-invocation `--diagnostic` capture that buffers data locally for review before export. |

`diagnostic` is not a persistent level. It is an overlay on `detailed` and is
only ever enabled by an explicit `--diagnostic` flag.

## What is collected

### Basic metrics

- `agent_marketplace.command.invocations` — counter
- `agent_marketplace.command.duration_ms` — histogram

Resource attributes (on every request):

- `telemetry.schema.version`
- `installation.id` — random, resettable per-installation identifier
- `cli.version`
- `os.name`
- `arch`

Metric point attributes:

- `command.name` — command path, e.g. `agent-marketplace sync`
- `harnesses` — comma-separated configured harness targets
- `managed` — whether the installation is organization-managed
- `outcome.code` — `success` or `error`

### Detailed inventory metrics

A successfully resolved effective configuration produces three integer gauges
plus one omission counter:

- `agent_marketplace.config.snapshot` — one complete observed configuration;
  its value is a Unix-microsecond observation ordering key, made strictly
  increasing within one CLI runtime;
- `agent_marketplace.config.registry` — one configured registry in that
  configuration, with value `1`;
- `agent_marketplace.config.resource` — one effectively enabled resource in
  that configuration, with value `1`; and
- `agent_marketplace.config.inventory_omissions` — count of whole snapshots not
  emitted, with bounded `reason` values `inventory_too_large`,
  `invalid_inventory`, and `queue_full`.

The registry and resource gauges contain sanitized registry identity and
location and exact enabled resource names. All four inventory instruments are
collected **only at the `detailed` level and only on the primary telemetry
route**. They are never part of `basic` telemetry and never flow to the
community-report route, including when detailed primary telemetry and community
reporting are both enabled. This additive contract is telemetry schema major
`1`, minor `1`.

All points for a configuration share `config.fingerprint`, the lowercase
hexadecimal SHA-256 digest of a canonical telemetry-safe representation.
Harnesses, registries, and resources are sorted by all represented fields before
hashing, so input iteration order does not affect the fingerprint. Returning to
a previously reported telemetry-visible configuration reuses its fingerprint;
it does not identify an installation and is meaningful only together with the
resettable `installation.id` and observation time.

Registry attributes contain a canonical `registry.id`, sanitized
`registry.location`, remote-only `registry.path`, configured `registry.ref`,
configuration scope, and bounded boolean fields. Equivalent URL and SCP-style
Git transports aggregate under one identity. URL user information, query
strings, and fragments are removed, and every local path or `file://` registry
is represented as `local` without a path-derived identifier. Resource points
add the exact `resource.kind` and `resource.name` for effectively enabled
resources. They do not contain resource contents, descriptions, tags, prompts,
or registry credentials.

Inventory is observed only after catalog loading and effective resource
resolution succeed. Hard resolution errors produce no inventory snapshot; soft
warnings do not prevent one. A mutating flow reports the post-mutation
resolution, and ad-hoc `--from` browsing outside effective configuration is not
inventory. Recording enqueues the complete generation for prompt background
export, so network I/O does not block the resolving caller. Inventory uses delta
temporality and forces an export after each queued observation; this preserves
successive observations of an unchanged gauge series during a long-lived TUI or
other session instead of waiting for the normal periodic reader interval.

A report is an observation, not a lease: if the CLI does not run, no new
snapshot asserts that the configuration remains current. Backends and
dashboards therefore select the greatest snapshot gauge value per installation
at the evaluation time within an operator-chosen freshness window, then retain
registry and resource points with that snapshot's fingerprint. Installations
with no observation in that window are excluded.

Inventory dimensions are intentionally higher-cardinality than the basic
contract. Active series grow approximately with reporting installations times
one snapshot plus represented registries, enabled resources, and up to three
observed omission reasons; retained series also grow with distinct fingerprints
and resource-attribute versions. The client accepts at most 1,000 registries
and 10,000 resources, and every string attribute is limited to 512 UTF-8 bytes.
Exceeding any bound increments the omission counter with `inventory_too_large`
and omits the whole snapshot rather than sending a misleading partial
configuration. Invalid inventory uses
`invalid_inventory`; a full prompt-export queue uses `queue_full`.
Fingerprinting or recording failure remains best effort and does not affect
command output, filesystem behavior, or exit status.

### Detailed traces

Spans include lifecycle stages such as `agent_marketplace.command`,
`agent_marketplace.tui.session`, `agent_marketplace.tui.action`, configuration
snapshots, and pipeline stages. Configuration snapshots include the effective
harness list, configured remote registry locations, and exact enabled resource
names grouped by kind, registry, and configuration scope. Registry locations
omit URL user information, query strings, and fragments; local registries are
reported only as `local`. Large resource sets are split into bounded chunks.
The following are **never** attached to telemetry:

- command arguments
- registry credentials or local registry paths
- resource contents, descriptions, or tags
- file paths (except the diagnostic review payload written locally)
- user names, hostnames unrelated to a configured remote registry, or network addresses
- prompts, terminal contents, search text, or subprocess output

### Diagnostic capture

`--diagnostic` buffers metrics, traces, and selected events in memory, writes a
JSON review payload under `$XDG_STATE_HOME/agent-marketplace/diagnostic/`, and
optionally exports it to the primary endpoint. The payload is redacted to
remove URL credentials from every string attribute and event message before it
is written or sent.

## Setup

### Unmanaged (personal) setup

Run interactive init and enable telemetry:

```bash
agent-marketplace init
agent-marketplace telemetry enable --level basic
```

Or use environment variables for a single invocation (they do not persist):

```bash
AGENT_MARKETPLACE_TELEMETRY_LEVEL=basic \
AGENT_MARKETPLACE_TELEMETRY_ENDPOINT=https://otel.example.com/v1 \
  agent-marketplace sync
```

`AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING=true` sends the basic schema to
the published project endpoint in addition to the primary endpoint.

### Managed (organization) setup

An organization config can enforce a minimum level and route:

```yaml
apiVersion: agent-marketplace/v2
harnesses:
  - factory
telemetry:
  level: detailed
  endpoint: https://otel.example.com/v1
  community_reporting: false
  ca_file: /etc/example/otel-ca.pem
  allow_insecure: false
  headers:
    Authorization:
      env: AGENT_MARKETPLACE_TELEMETRY_AUTH_TOKEN
    X-Tenant:
      file: /etc/example/tenant
```

Organization settings cannot be overridden by user config or environment
variables. `telemetry status` reports which fields are locked.

## Telemetry commands

- `agent-marketplace telemetry status` — effective level, endpoint authority,
  installation ID, pending records, last failure, and locked fields.
- `agent-marketplace telemetry enable --level <basic|detailed> [--endpoint <url>]`
  — enable telemetry at the user level.
- `agent-marketplace telemetry disable` — set user level to `off`.
- `agent-marketplace telemetry preview --level <basic|detailed|diagnostic>` —
  write a representative payload to a local file without sending it.
- `agent-marketplace telemetry reset-id` — generate a new installation ID and
  purge queued payloads.
- `agent-marketplace telemetry purge` — remove queued records and retained
  diagnostic payloads.

## Diagnostic payload review

Run a command with `--diagnostic` to capture a reviewed payload:

```bash
agent-marketplace --diagnostic --diagnostic-accept-sensitive-data sync
```

`--diagnostic-accept-sensitive-data` is required in non-interactive mode. In
interactive mode the CLI prompts for confirmation.

The CLI writes the payload to `$XDG_STATE_HOME/agent-marketplace/diagnostic/payload-<trace-id>.json`
and prints:

- destination hostname
- retention policy URL
- installation ID
- trace ID
- sensitivity warning

Review the JSON file before sharing. Export deletes the local file; a failed
export keeps it for manual inspection. Diagnostic export failure never changes
the command exit status or queues the payload for normal delivery.

## Retention

- Collector-side retention is controlled by the OTEL collector deployment.
- Queued payloads on the CLI: 7 days or 5 MiB, whichever comes first.
- Diagnostic review payloads: deleted after successful export; retained on
  failure.
- Installation ID: stored until `telemetry reset-id`.

## Troubleshooting

- Check status: `agent-marketplace telemetry status`.
- Last delivery failure is shown in status and also written to
  `$XDG_STATE_HOME/agent-marketplace/telemetry/last-failure.json`.
- Pending records indicate queued exports; the next telemetry-enabled command
  drains them oldest-first.
- Use `telemetry purge` to remove queued records and diagnostic payloads.
- Run with `--debug` to see telemetry setup and export logs on stderr.

## Reset and purge

```bash
# Generate a new installation ID and remove queued payloads
agent-marketplace telemetry reset-id

# Remove queued records and diagnostic payloads without changing the installation ID
agent-marketplace telemetry purge
```

## Security and privacy

- Telemetry is opt-in and never sends command arguments, resource contents, or
  terminal contents.
- URL credentials are stripped from every string value before a diagnostic
  payload is written or exported.
- Standard `OTEL_*` environment variables are ignored for policy resolution to
  prevent silent bypass of organization settings.
- The optional project report contains only the basic schema.
- Detailed data, including inventory, and diagnostic data never flow to the
  project report.
- Treat installation-level detailed inventory access as sensitive:
  `installation.id` is pseudonymous and resettable, but it links reports over
  time and a drill-down reveals exact enabled resource names, remote registry
  identities, scopes, harnesses, and configuration changes.

## Performance

Existing telemetry overhead was measured on an Apple M5 Pro with
`go test -bench`. The first table predates configuration inventory metrics:

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| Counter add | 233 | 232 | 5 |
| Histogram record | 247 | 232 | 5 |
| Start span | 1,157 | 1,891 | 15 |
| Start span (command path) | 1,399 | 2,520 | 17 |
| Record TUI action span | 1,227 | 2,346 | 17 |
| Resolve telemetry config | 3,923 | 3,319 | 20 |
| Get telemetry status | 170,094 | 12,900 | 76 |
| New + shutdown runtime | 275,125 | 54,606 | 489 |
| Queue push + drain | 503,619 | 11,644 | 83 |
| Load installation ID | 19,334 | 984 | 8 |
| Diagnostic redaction | 4,199 | 4,030 | 65 |
| Diagnostic payload serialization | 2,611 | 2,164 | 24 |
| Diagnostic URL redaction | 2,133 | 583 | 23 |

Numbers are from one-second benchmark runs and are intended as a rough
indication of overhead. The fast path (`CounterAdd`, `HistogramRecord`,
`StartSpan`) is measured in hundreds of nanoseconds.

Inventory-specific measurements are also workload-specific and should not be
read as production latency guarantees:

- `BenchmarkRecordInventory` measured approximately 0.60–0.61 ms/op,
  1.186 MB/op, and 5,264–5,267 allocs/op. Its fixture has 10 registries and 100
  resources, detailed telemetry, and a local OTLP HTTP endpoint. It measures
  accepted observation enqueueing while prompt background recording and export
  proceed; it is not a maximum-size inventory or remote-network benchmark.
- `BenchmarkResolveInventoryWithLocalDiskAndHTTPExport` measured a median
  3.155 ms/op, 1,714,779 B/op, and 12,592 allocs/op. The fixture resolves 50
  selected skills from one local registry and waits for each inventory snapshot
  to reach a local OTLP HTTP endpoint, preventing queue saturation from turning
  the benchmark into an omission-path measurement. Local registry disk I/O and
  local HTTP export are included; remote registry access and materialization are
  excluded.
