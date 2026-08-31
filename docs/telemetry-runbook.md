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
- Retry deduplication retains at most 1,000,000 request IDs by default. Use `--max-request-ids` to adjust the bound; new IDs are rejected with HTTP `429` at capacity while known retries remain idempotent.

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
- Retained-total panels use cumulative counters and show the first accepted event. Rate and rate-derived percentile panels require at least two scrapes and intentionally describe observed change rather than retained totals.

The receiver restores retained Prometheus counters, duration histograms, adoption totals, last-receive time, rolling active-installation windows, and admitted series from its JSONL files after a restart. Export requests carry a durable request ID, so replay after an ambiguous HTTP response returns success without applying the same delta twice. Daily records outside the retained 30 UTC date partitions are pruned automatically at startup and on the first request each UTC day. Run one receiver process per storage directory; startup fails while another process holds that directory's advisory lock. The receiver requires Unix advisory locking and is not supported on Windows.

For a local end-to-end stack, run `make telemetry-up`. It starts the receiver, Prometheus, and Grafana with the datasource and dashboard provisioned; Grafana is available at `http://127.0.0.1:3000/d/agent-marketplace-telemetry`. Run `docker compose -f compose.telemetry.yaml run --rm smoke-event` to emit one CLI event, `make telemetry-smoke` for an isolated end-to-end check, and `make telemetry-down` to stop the persistent stack without deleting its volumes. This loopback-only development stack enables anonymous Grafana Viewer access and must not be exposed through a proxy or public interface.

Environments that mirror public images can set `TELEMETRY_GO_BUILD_IMAGE`, `TELEMETRY_ALPINE_RUNTIME_IMAGE`, `TELEMETRY_PROMETHEUS_IMAGE`, and `TELEMETRY_GRAFANA_IMAGE`. Host ports can be changed with `TELEMETRY_RECEIVER_PORT`, `TELEMETRY_PROMETHEUS_PORT`, and `TELEMETRY_GRAFANA_PORT`.

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
