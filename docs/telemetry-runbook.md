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
- Receiver health is available at `GET /health` and dashboard summaries at `GET /v1/dashboard.json`.

## Cardinality budget

- The default installation-ID cardinality budget is **10,000** unique `installation.id` values.
- The receiver returns HTTP `429` when the budget is exceeded and logs a warning.
- The client-side `ValidateCardinalityBudget` helper documents the contract; the server is the enforcing authority.
- To raise the budget, restart the receiver with `--max-installations`.

## Dashboards

- `GET /health` returns `{"status":"ok","last_receive":"..."}`.
- `GET /v1/dashboard.json` returns a JSON summary:
  - `adoption.commands` — command invocation count by `command.name`.
  - `adoption.harnesses` — command invocation count by individual harness target.
  - `health.unique_installations` — current unique `installation.id` count.
  - `health.storage_size_bytes` — total byte size of the receiver storage directory.
  - `health.metric_count` — total number of accepted metric data points.

## Starting the receiver

```bash
agent-marketplace telemetry receiver
agent-marketplace telemetry receiver --listen localhost:4318 --storage-dir /var/lib/am-telemetry --max-installations 10000
```

The receiver logs the listen address, dashboard URL, and storage directory on startup. Press Ctrl-C to trigger a graceful shutdown.

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
