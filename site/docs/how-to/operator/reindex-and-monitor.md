# How to reindex and monitor a Marketplace

Refresh registry data on demand and watch the health of a hosted Marketplace.

## Before you reindex

The worker refreshes every configured registry when it starts and again at each `refresh.interval` (30 minutes by default). Use `serve reindex` when you need a refresh now, for example after a configuration change.

`serve reindex` opens the stores, queues one refresh job per registry, and waits for each job to finish. It processes a job in its own process unless a running worker leases it first, so it may clone the registry itself. Run it where the worker runs, with the same configuration file, credentials, network access, and Git. The API image does not contain Git. For example, in the repository's SQLite Compose setup:

```bash
docker compose exec app /app/agent-marketplace serve reindex --config /app/marketplace.yaml
```

## Reindex one registry

Pass a registry ID or slug:

```bash
agent-marketplace serve reindex shared-agents --config /app/marketplace.yaml
```

## Reindex every registry

```bash
agent-marketplace serve reindex --config /app/marketplace.yaml
```

With `management.mode: file`, this reindexes every registry in the configuration file. With `management.mode: database`, it reindexes the active admin-managed registries and ignores the `registries:` list.

Before it clones anything, `serve reindex` reconciles the stored revocations with the configuration it was given. After processing, it checks that every published artifact has a matching blob. The command exits with an error if any registry fails or the integrity check fails, and gives up after one hour.

## Check health endpoints

- `/healthz` on the public listener returns `{"status":"ok"}` while the process is running.
- `/readyz` on the operations listener returns `{"status":"ready"}` only when all of these hold: the database's recorded configuration authority matches `management.mode`, the stored revocations match the process's configuration, the Marketplace record is readable from the metadata store, and the blob store responds. Otherwise it returns `503`.
- `/metrics` on the operations listener returns Prometheus metrics.

Only processes that run the API (`--role api` or `--role all`) serve these endpoints. A `--role worker` process has no HTTP listener. Do not expose `/readyz` or `/metrics` through the public reverse proxy.

## Watch the logs

Look for these values of the `code` attribute:

- `marketplace.ingest.duplicate-resource`: two paths in one registry publish the same kind and name. The worker keeps the first and logs both paths.
- `marketplace.policy.license-not-allowed`: the license allowlist rejected a resource.
- `marketplace.policy.license-undeclared`: a resource declares no license.
- `marketplace.policy.artifact-too-large`: an artifact exceeded `artifact_bytes`.

Also watch for these messages:

- `serve failed` with reason `open stores`: the process could not open storage, which includes a failed database migration.
- `resource revision archive digest repaired`: a refresh staged unchanged content under a different archive digest. The stored digest was repaired and the previous artifact is still downloadable; no action is needed.
- `managed refresh attempt failed`: a managed indexing attempt failed. The line carries `failure_code` (a stable classification) and `cause` (sanitized, without raw command output or credentials); the same pair appears on the attempt in `/api/v1/admin/attempts`.
- `process refresh job failed` and `reindex failed`: a registry refresh failed. The worker message puts the cause in the `err` attribute, and `serve reindex` puts it in `error`.

## Alert on metrics

The API exposes these application metrics, alongside the standard Go runtime and process metrics:

- `agent_marketplace_requests_total`: counter of HTTP requests, labeled by `listener`, `method`, `route`, and `code`. Use it for request rate and error rate on requests that reach routing.
- `agent_marketplace_request_duration_seconds`: histogram of request duration, labeled by `listener`, `method`, and `route`. Use it for routed-request latency.
- `agent_marketplace_active_requests`: gauge of in-flight requests, labeled by `listener`.
- `agent_marketplace_public_request_duration_seconds`: histogram of the full public request stack, labeled by `method`, `route`, and `code`. Unlike the three series above, it also records the early `503` rejections that the configuration authority and availability checks produce before routing, so use it for full public latency and early-failure counts.
- `agent_marketplace_request_phase_duration_seconds`: histogram labeled by `phase` (`authority`, `availability`, `catalog_read`, `catalog_availability`, `catalog_response`). Use it to attribute public catalog time to a stage; a phase is recorded only when that work ran.
- `agent_marketplace_metadata_pool_waits_total` and `agent_marketplace_metadata_pool_wait_seconds_total`: cumulative metadata connection pool waits and wait seconds since the pool opened. They show pool contention only; they are not a general storage-saturation signal.

The `route` label holds the registered route pattern, or `unmatched`, never the raw request path. None of these metrics cover queue age, general storage saturation, or reindex health. Get those signals from application logs, the reverse proxy, or the platform that runs the containers. The Helm chart can create a `ServiceMonitor` that scrapes the API's `operations` port (`serviceMonitor.enabled`).

## Related documentation

- [How to reset and reindex a registry](./reset-and-reindex-a-registry.md)
- [Marketplace health and metrics reference](../../reference/marketplace-health-and-metrics.md)
