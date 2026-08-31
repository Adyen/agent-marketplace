# Telemetry Runbook

This runbook covers the community telemetry receiver, endpoint configuration, authentication, reliability, cardinality limits, and dashboards.

## Endpoint and TLS

The community telemetry endpoint is configured at release build time using `-X` linker flags:

- `github.com/Adyen/agent-marketplace/internal/telemetry.DefaultProjectEndpoint`
- `github.com/Adyen/agent-marketplace/internal/telemetry.DefaultProjectRetentionURL`

Set these values through the environment variables used by `make build-release` or `scripts/build-release.sh`:

- `AGENT_MARKETPLACE_RELEASE_TELEMETRY_ENDPOINT`
- `AGENT_MARKETPLACE_RELEASE_TELEMETRY_RETENTION_URL`

Production endpoints must use HTTPS. The CLI rejects non-loopback HTTP endpoints unless organization policy explicitly allows a private insecure endpoint (`allow_insecure: true`). The receiver itself defaults to `localhost:4318` and is intended for local or private-network operation; place it behind a TLS-terminating reverse proxy for production use.

## Auth header handling

The CLI supports header values sourced from environment variables or permission-restricted files:

```yaml
telemetry:
  headers:
    Authorization:
      env: AGENT_MARKETPLACE_TELEMETRY_AUTH_TOKEN
    X-Tenant:
      file: /etc/otel/tenant
```

`telemetry status` and `telemetry preview` show header names and source types but never print values. The receiver does not require authentication by default; the production reverse proxy must enforce the expected authentication scheme.

## Reliability, queue, and backoff

- The CLI queues failed exports durably under `$XDG_STATE_HOME/agent-marketplace/telemetry/queue`.
- The queue is capped at 5 MiB and 7 days; oldest records are evicted first.
- Each enabled invocation drains the queue oldest-first before sending current metrics.
- Exported payloads do not use automatic SDK retries; failures are queued for the next invocation.
- Receiver health is available at `GET /health`, dashboard summaries at `GET /v1/dashboard.json`, and Prometheus metrics at `GET /metrics`.

## Cardinality budget

- The default installation-ID cardinality budget is **10,000** unique `installation.id` values.
- The receiver returns HTTP `429` when the budget is exceeded and logs a warning.
- The client-side `ValidateCardinalityBudget` helper documents the contract; the server is the enforcing authority.
- To raise the installation budget, restart the receiver with `--max-installations`.
- Prometheus adoption series have a separate 10,000-series default budget. Use `--max-series` to override it; new label combinations are rejected with HTTP `429` after the limit is reached.

## Dashboards

- `GET /health` returns `{"status":"ok","last_receive":"..."}`.
- `GET /v1/dashboard.json` returns a JSON summary:
  - `adoption.commands` — command invocation count by `command.name`.
  - `adoption.harnesses` — command invocation count by individual harness target.
  - `health.unique_installations` — current unique `installation.id` count.
  - `health.storage_size_bytes` — total byte size of the receiver storage directory.
  - `health.metric_count` — total number of accepted metric data points.
- `GET /metrics` exposes Prometheus counters, histograms, and gauges for adoption and receiver health. It never exposes `installation.id` as a label.
- Import [`monitoring/agent-marketplace-telemetry-dashboard.json`](../monitoring/agent-marketplace-telemetry-dashboard.json) into Grafana and select the Prometheus datasource and scrape job.
- Configure Prometheus or VictoriaMetrics to scrape the receiver's `/metrics` path. Keep this operator endpoint private even when `/v1/metrics` is available through a public ingress.

The receiver restores retained adoption totals, last-receive time, and rolling active-installation windows from its JSONL files after a restart. Prometheus counters intentionally restart from zero so `rate` and `increase` do not interpret replayed records as new traffic. Run one receiver process per storage directory; startup fails while another process holds that directory's advisory lock.

## Starting the receiver

```bash
agent-marketplace telemetry receiver
agent-marketplace telemetry receiver --listen localhost:4318 --storage-dir /var/lib/am-telemetry --max-installations 10000
```

The receiver logs the listen address, dashboard URL, and storage directory on startup. Press Ctrl-C to trigger a graceful shutdown.

## Diagnostic capture

Users can capture a reviewed diagnostic payload for a single invocation:

```bash
agent-marketplace --diagnostic --diagnostic-accept-sensitive-data list
```

The CLI writes the payload to
`$XDG_STATE_HOME/agent-marketplace/diagnostic/payload-<trace-id>.json` and
exports it only after confirmation. The payload contains redacted metrics,
traces, and selected events with URL credentials removed. A failed export does
not queue the payload for normal delivery.

## Troubleshooting

- `agent-marketplace telemetry status` shows effective level, endpoint,
  installation ID, pending records, retained diagnostic payloads, and the last
  delivery failure.
- Pending records indicate queued exports that will be drained on the next
  telemetry-enabled command.
- `agent-marketplace telemetry purge` removes queued records and retained
  diagnostic payloads.
- `agent-marketplace telemetry reset-id` rotates the installation ID and purges
  queued payloads.
- Run with `--debug` to see telemetry setup, queue drain, and export logs.

## Release build flags

Use the provided target or script to inject endpoint defaults without editing source files:

```bash
make build-release
# or
AGENT_MARKETPLACE_RELEASE_TELEMETRY_ENDPOINT=https://telemetry.example.com/v1/metrics \
AGENT_MARKETPLACE_RELEASE_TELEMETRY_RETENTION_URL=https://example.com/telemetry-retention.md \
  ./scripts/build-release.sh
```

Do not commit production URLs or secrets to the repository.
