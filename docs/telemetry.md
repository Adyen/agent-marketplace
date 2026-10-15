# Telemetry

> **Historical, non-normative lead — pending harvest.** This document retains
> pre-launch guidance and obsolete consumer configuration examples. It is not
> current launch usage or a normative contract. Use the current
> [telemetry reference](../site/docs/reference/telemetry.md),
> [telemetry configuration guide](../site/docs/how-to/configure-telemetry.md), and
> [consumer configuration reference](../site/docs/reference/configuration.md) instead.

`agent-marketplace` can send telemetry about command usage and health over
vendor-neutral OTLP/HTTP. Telemetry is off until a user opts in, and an
organization config can set it for every user of a managed installation. The
design separates three concerns: the level (how much is collected), the
authority (who decides), and the route (where it goes). See
[ADR 0001](adr/0001-telemetry-authority-and-routing.md).

## Telemetry levels

| Level | Signals | When to use |
|---|---|---|
| `off` | None | No telemetry is collected or sent. This is the default. |
| `basic` | Metrics | Command counts and durations with bounded, non-identifying attributes. |
| `detailed` | Metrics + traces | Basic metrics, primary-route configuration inventory gauges and omission counter, and lifecycle spans for commands, TUI, and pipeline stages. |
| `diagnostic` | Metrics + traces + events + logs | A per-invocation `--diagnostic` capture that buffers data locally for review before export. |

`diagnostic` is not a level you can store in configuration; the config
validator rejects it. It is an overlay on `detailed` that only the
`--diagnostic` flag turns on. Capture runs only when the effective level is
`detailed`. At any other level the flag has no effect; at `basic` the CLI also
logs a warning.

## What is collected

### Basic metrics

- `agent_marketplace.command.invocations` (counter)
- `agent_marketplace.command.duration_ms` (histogram)

Resource attributes (on every request):

- `telemetry.schema.version`
- `installation.id`: a random per-installation identifier that you can reset
- `cli.version`
- `os.name`
- `arch`

Metric point attributes:

- `command.name`: the command path, for example `agent-marketplace sync`
- `harnesses`: configured harness targets, sorted, deduplicated, and
  comma-separated (from the user config, or the organization config when the
  user config has none)
- `managed`: whether the installation is organization-managed
- `outcome.code`: `success` or `error`

### Detailed inventory metrics

A successfully resolved effective configuration produces three integer gauges
and one omission counter:

- `agent_marketplace.config.snapshot`: one complete observed configuration.
  Its value is a Unix-microsecond observation ordering key, strictly
  increasing within one CLI runtime.
- `agent_marketplace.config.registry`: one configured registry in that
  configuration, with value `1`.
- `agent_marketplace.config.resource`: one effectively enabled resource in
  that configuration, with value `1`.
- `agent_marketplace.config.inventory_omissions`: the count of whole snapshots
  not emitted, with a bounded `reason` of `inventory_too_large`,
  `invalid_inventory`, or `queue_full`.

The registry and resource gauges contain sanitized registry identity and
location plus exact enabled resource names. The CLI collects all four
inventory instruments only at the `detailed` level and only on the primary
route. They are never part of `basic` telemetry and never go to the community
report route, even when detailed primary telemetry and community reporting are
both enabled. This addition is telemetry schema major `1`, minor `1`.

All points for one configuration share `config.fingerprint`, the lowercase
hexadecimal SHA-256 digest of a telemetry-safe representation of that
configuration. Harnesses, registries, and resources are sorted by every
represented field before hashing, so input order does not change the
fingerprint. Returning to a configuration that was reported before reuses its
fingerprint. The fingerprint does not identify an installation; it only has
meaning together with the resettable `installation.id` and the observation
time.

Registry points carry a normalized `registry.id`, a sanitized
`registry.location`, `registry.path` (remote registries only), the configured
`registry.ref`, the configuration scope, and bounded boolean fields. HTTPS,
SSH URL, and SCP-style Git addresses for the same repository share one
identity. The CLI removes URL user information, query strings, and fragments,
and reports every local path or `file://` registry as `local`, with no
identifier derived from the path. Resource points add the exact
`resource.kind` and `resource.name` of each effectively enabled resource. They
never contain resource contents, descriptions, tags, prompts, or registry
credentials.

The CLI records inventory only after catalog loading and effective resource
resolution succeed. A hard resolution error produces no snapshot; soft
warnings do not prevent one. A command that changes configuration reports the
resolution after the change. Ad-hoc `--from` browsing is outside the effective
configuration and is not inventory. Recording hands the complete snapshot to a
background exporter, so network I/O does not block the command. Inventory uses
delta temporality and forces an export after each queued observation. That way
a long-lived TUI session reports every observation of an unchanged gauge
series instead of waiting for the periodic reader interval.

A report is an observation, not a lease: when the CLI does not run, nothing
asserts that the configuration is still current. A backend or dashboard should
therefore pick, per installation, the greatest snapshot gauge value within a
freshness window the operator chooses, then keep the registry and resource
points with that snapshot's fingerprint. Installations with no observation in
the window drop out.

Inventory dimensions have higher cardinality than the basic metrics. Active
series grow roughly with the number of reporting installations times (one
snapshot plus the represented registries, the enabled resources, and up to
three omission reasons). Retained series also grow with distinct fingerprints
and resource-attribute versions. The client accepts at most 1,000 registries
and 10,000 resources, and limits every string attribute to 512 UTF-8 bytes.
Exceeding any limit increments the omission counter with
`inventory_too_large` and drops the whole snapshot, so a backend never
receives a misleading partial configuration. Invalid inventory is counted as
`invalid_inventory`, and a full export queue as `queue_full`. Fingerprinting
and recording are best effort: a failure never changes command output,
filesystem behavior, or exit status.

### Detailed traces

Spans cover lifecycle stages such as `agent_marketplace.command`,
`agent_marketplace.tui.session`, `agent_marketplace.tui.action`, configuration
snapshots, and pipeline stages (`agent_marketplace.pipeline.*`). Configuration
snapshots include the effective harness list, configured remote registry
locations, and exact enabled resource names grouped by kind, registry, and
configuration scope. Registry locations omit URL user information, query
strings, and fragments, and local registries appear only as `local`. Large
resource sets are split into bounded chunks.

The CLI never attaches any of the following to telemetry:

- command arguments
- registry credentials or local registry paths
- resource contents, descriptions, or tags
- file paths (except in the diagnostic review payload written locally)
- user names, hostnames unrelated to a configured remote registry, or network addresses
- prompts, terminal contents, search text, or subprocess output

### Diagnostic capture

`--diagnostic` buffers metrics, traces, selected events, and log records in
memory instead of sending them through the normal exporters. At the end of the
command the CLI writes a JSON review payload to
`$XDG_STATE_HOME/agent-marketplace/telemetry/diagnostic/` and, with consent,
exports it to the primary endpoint. Before writing or sending, it strips URL
credentials from every string attribute and event message.

Log capture records every log record of the invocation at debug level and
above, whatever the configured log level. The log file and stderr keep their
configured levels. Capture covers the TUI too. The CLI keeps at most 2,000
records, whose bounded messages, attribute keys, and attribute values total at
most 4 MiB. It keeps records in emission order and stops at the first record
that does not fit, so every later record counts in `logs_dropped`. A message is
truncated to 8 KiB and an attribute value to 2 KiB, and each cut ends in
`…[truncated]`. Group attributes become dotted keys. Keys must match
`^[a-z][a-z0-9_.]{0,63}$`, and each record keeps its first 32 attributes. Each
record carries the invocation's trace ID. Its span ID is the span active in the
record's context when that span belongs to the invocation, and the root command
span's ID otherwise. Runs without `--diagnostic`, or with it at level `basic`,
capture and send no logs.

## Setup

### Unmanaged (personal) setup

The first run of interactive `agent-marketplace init` (without `--harnesses`)
creates the user config and asks whether to enable basic telemetry. You can
also enable it later; `telemetry enable` requires an existing user config:

```bash
agent-marketplace init
agent-marketplace telemetry enable --level basic
```

Without `--endpoint`, the primary route uses the project endpoint compiled
into the binary. Only `make build-release` with
`AGENT_MARKETPLACE_RELEASE_TELEMETRY_ENDPOINT` set compiles one in. `make
build` and `make release` (which produces the published GitHub release
binaries) leave it empty, so on those builds no data leaves the machine until
you configure an endpoint. In that case the `Primary endpoint:` line of
`telemetry status` shows `none`.

For a single invocation you can use environment variables instead; they do
not persist:

```bash
AGENT_MARKETPLACE_TELEMETRY_LEVEL=basic \
AGENT_MARKETPLACE_TELEMETRY_ENDPOINT=https://otel.example.com \
  agent-marketplace sync
```

`AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING=true` also sends the basic
metrics to the project endpoint, alongside the primary endpoint. It has no
effect when the level is `off` or the binary has no project endpoint. When the
primary endpoint is the project endpoint, the CLI sends once instead of twice
("folded into primary" in `telemetry status`).

Give the endpoint as a base URL. The CLI sends metrics to `/v1/metrics` and
traces to `/v1/traces` on it. A URL that already has a path sends both signals
to that path unchanged.

A user or environment endpoint must use HTTPS, or HTTP to a loopback address.
Plain HTTP to a private address or hostname requires `allow_insecure: true`
from organization policy.

### Managed (organization) setup

An organization config can set the level and route for every user:

```yaml
apiVersion: agent-marketplace/v2
harnesses:
  - factory
telemetry:
  level: detailed
  endpoint: https://otel.example.com
  community_reporting: false
  ca_file: /etc/example/otel-ca.pem
  allow_insecure: false
  headers:
    Authorization:
      env: AGENT_MARKETPLACE_TELEMETRY_AUTH_TOKEN
    X-Tenant:
      file: /etc/example/tenant
```

Each field the organization sets wins over user config and environment
variables:

- `level` fixes the effective level, in both directions. An organization
  `off` keeps telemetry off even if the user enables `detailed`, and an
  organization `basic` keeps it at `basic`. `telemetry enable` refuses a level
  below the organization level. When an organization `telemetry:` block exists
  at all, `AGENT_MARKETPLACE_TELEMETRY_LEVEL` is ignored, even if the block has
  no `level`.
- `endpoint` replaces the primary endpoint. The CLI ignores
  `AGENT_MARKETPLACE_TELEMETRY_ENDPOINT`, and `telemetry enable --endpoint`
  fails with `endpoint is locked by organization policy`. The organization's `headers`, `ca_file`, and `allow_insecure` apply to that
  endpoint.
- `community_reporting` ignores the user setting and the environment
  variable. When the organization block omits it, the user may still enable
  community reporting.
- `retention_policy` (a URL) is shown to users as the retention policy of the
  organization endpoint.

`allow_insecure` only has an effect in the organization layer. It permits
plain HTTP to private, loopback, or link-local addresses; a hostname must
resolve only to such addresses at delivery time.

Headers take their values from an environment variable (`env`) or a file
(`file`), never inline. When a `ca_file` or header `file` exists, it must be a
regular file (not a symlink) owned by the user running the CLI, with no group
or other permission bits, in a parent directory that meets the same rules.
Otherwise telemetry resolution fails, and the command continues without
telemetry.

`telemetry status` lists the fields the organization has locked. Telemetry is
allowed only in the organization and user configs; repository and
personal-local configs that declare it fail validation.

## Telemetry commands

- `agent-marketplace telemetry status`: effective level and its source,
  primary endpoint host and source, header sources, retention policy,
  community reporting, managed flag, installation ID, pending records,
  retained diagnostic payloads, last delivery failure, and locked fields.
- `agent-marketplace telemetry enable --level <basic|detailed> [--endpoint <url>]`:
  enable telemetry in the user config.
- `agent-marketplace telemetry disable`: set the user level to `off`. If
  organization policy keeps telemetry on, the command says so.
- `agent-marketplace telemetry preview --level <basic|detailed|diagnostic>`:
  write a representative payload to
  `$XDG_STATE_HOME/agent-marketplace/telemetry/preview/` without sending it.
  A `diagnostic` preview asks for confirmation, or needs
  `--diagnostic-accept-sensitive-data` when not run in a terminal.
- `agent-marketplace telemetry reset-id`: delete the installation ID, queued
  records, and retained diagnostic payloads, then generate a new ID.
- `agent-marketplace telemetry purge`: remove queued records and retained
  diagnostic payloads.

`init` and the `telemetry` commands themselves never send telemetry.

## Diagnostic payload review

Run a command with `--diagnostic` to capture a payload for review:

```bash
agent-marketplace --diagnostic --diagnostic-accept-sensitive-data sync
```

The CLI writes the payload to
`$XDG_STATE_HOME/agent-marketplace/telemetry/diagnostic/payload-<trace-id>.json`
and prints:

- destination hostname
- retention policy URL
- installation ID
- trace ID
- sensitivity warning

The payload's `logs` array lists the captured log records, each with
`timestamp`, `severity` (`DEBUG`, `INFO`, `WARN`, or `ERROR`), `message`,
`trace_id`, `span_id`, and `attributes`. `logs_dropped` counts the records
beyond either bound: the 2,000-record count or the 4 MiB total. The payload's
`schema_version` stays `1`.

In a terminal, the CLI then asks `Proceed with export? [y/N]`. Without a
terminal, it exports only when you pass `--diagnostic-accept-sensitive-data`;
otherwise it keeps the file and prints where it is. Review the JSON file
before sharing it. A successful export deletes the local file. A failed or
declined export keeps it for manual inspection. Export needs a primary
endpoint. On consent, the CLI sends the log records as OTLP logs to the
primary endpoint's `/v1/logs`, next to metrics and traces. The primary endpoint
must accept `/v1/logs`. If it rejects that request, the export fails and the
payload is kept. Diagnostic export never changes the command's exit status, and
the CLI never queues the payload for normal delivery.

## Retention

- The OTEL collector deployment controls collector-side retention.
- Queued payloads on the CLI: 7 days or 5 MiB, whichever limit is reached
  first; the oldest records go first.
- Diagnostic review payloads: deleted after a successful export, kept
  otherwise.
- Installation ID: kept until `telemetry reset-id`.

## Troubleshooting

All telemetry state lives under `$XDG_STATE_HOME/agent-marketplace/telemetry/`
(by default `~/.local/state/agent-marketplace/telemetry/`): the
`installation-id` file and the `queue/`, `diagnostic/`, and `preview/`
directories.

- Check status: `agent-marketplace telemetry status`.
- `telemetry status` shows the last delivery failure, which the CLI also
  writes as JSON to
  `$XDG_STATE_HOME/agent-marketplace/telemetry/last-delivery-failure`.
- Pending records are queued exports. The next telemetry-enabled command
  drains them oldest first.
- If telemetry configuration fails to resolve (for example, an invalid
  endpoint or a header file with loose permissions), commands keep working
  and the log records `telemetry config resolution failed; continuing without
  telemetry`. `telemetry status` prints the error.
- The CLI ignores standard `OTEL_*` environment variables and logs a warning
  for each one it sees.
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
- The CLI strips URL credentials from every string value before it writes or
  exports a diagnostic payload.
- Standard `OTEL_*` environment variables play no part in policy resolution,
  so they cannot silently bypass organization settings.
- The optional community report contains only the basic metrics.
- Detailed data (including inventory) and diagnostic data never go to the
  community report.
- Treat access to per-installation detailed inventory as sensitive.
  `installation.id` is pseudonymous and resettable, but it links reports over
  time, and a drill-down reveals exact enabled resource names, remote registry
  identities, scopes, harnesses, and configuration changes.

## Performance

Telemetry overhead was measured on an Apple M5 Pro with `go test -bench`. The
first table was recorded before configuration inventory metrics existed:

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

These numbers come from one-second benchmark runs and give a rough idea of
overhead. The fast path (`CounterAdd`, `HistogramRecord`, `StartSpan`) takes
hundreds of nanoseconds.

The inventory measurements below depend on their fixtures and are not
production latency guarantees:

- `BenchmarkRecordInventory` measured about 0.60 to 0.61 ms/op, 1.186 MB/op,
  and 5,264 to 5,267 allocs/op. Its fixture has 10 registries, 100 resources,
  detailed telemetry, and a local OTLP HTTP endpoint. It measures how long an
  accepted observation takes to enqueue while background recording and export
  proceed. It is not a maximum-size inventory or remote-network benchmark.
- `BenchmarkResolveInventoryWithLocalDiskAndHTTPExport` measured a median of
  3.155 ms/op, 1,714,779 B/op, and 12,592 allocs/op. The fixture resolves 50
  selected skills from one local registry and waits for each inventory
  snapshot to reach a local OTLP HTTP endpoint, so queue saturation cannot
  turn it into a measurement of the omission path. It includes local registry
  disk I/O and local HTTP export, and excludes remote registry access and
  materialization.
