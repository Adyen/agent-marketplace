# Telemetry

`agent-marketplace` can send telemetry about command usage and health over
vendor-neutral OTLP/HTTP. Telemetry is opt-in for unmanaged installations and
can be locked by organization policy for managed installations.

For the community receiver retention and access policy, see
[`telemetry-retention.md`](./telemetry-retention.md). For operator runbook
information, see [`telemetry-runbook.md`](./telemetry-runbook.md).

## Telemetry levels

| Level | Signals | When to use |
|---|---|---|
| `off` | None | No telemetry is collected or sent. |
| `basic` | Metrics | Command counts and durations with bounded, non-identifying attributes. |
| `detailed` | Metrics + traces | Basic metrics plus lifecycle spans for commands, TUI, and pipeline stages. |
| `diagnostic` | Metrics + traces + events | A per-invocation `--diagnostic` capture that buffers data locally for review before export. |

`diagnostic` is not a persistent level. It is an overlay on `detailed` and is
ever enabled by an explicit `--diagnostic` flag.

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

### Detailed traces

Spans include lifecycle stages such as `agent_marketplace.command`,
`agent_marketplace.tui.session`, `agent_marketplace.tui.action`, and pipeline
stages. Attributes are limited to command path, harness list, managed flag,
outcome, resource kind, action name, and result count. The following are
**never** attached to telemetry:

- command arguments
- resource names, repository names, or registry URLs
- file paths (except the diagnostic review payload written locally)
- user names, hostnames, or network addresses
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
apiVersion: agent-marketplace/v3
harnesses:
  - factory
telemetry:
  level: detailed
  endpoint: https://otel.example.com/v1
  retention_policy: https://example.com/telemetry-retention.md
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

- Community endpoint data: 30 days.
- Queued payloads on the CLI: 7 days or 5 MiB, whichever comes first.
- Diagnostic review payloads: deleted after successful export; retained on
  failure.
- Installation ID: stored until `telemetry reset-id`.

See [`telemetry-retention.md`](./telemetry-retention.md) for the full policy.

## Troubleshooting

- Check status: `agent-marketplace telemetry status`.
- Last delivery failure is shown in status and also written to
  `$XDG_STATE_HOME/agent-marketplace/telemetry/last-delivery-failure`.
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
- The community report contains only the basic schema.
- Detailed and diagnostic data never flow to the community report.

## Performance

Overhead was measured on an Apple M5 Pro with `go test -bench`. All paths are
single-digit microseconds or less for the hot path, and allocation counts are
small enough that telemetry does not dominate command runtime.

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
