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

A successful reindex prints nothing to the terminal and exits `0`. Its records go to the JSON log file (`~/.agent-marketplace/logs/agent-marketplace.log` by default; see the [logging reference](../../reference/logging.md)), and a run ends with a `reindex complete` record. Typical records for one registry:

```json
{"msg":"reindexing registry","registry":"src-shared"}
{"msg":"refreshed registry","registry":"src-shared","commit":"1f9a3c2…","resources":27}
{"msg":"reindex complete","registries":1}
```

Run with `--debug` to print the same records to standard error as readable text. When the log file cannot be written, for example inside a container without a writable home directory, the process prints a `using stderr fallback` notice and sends the records to standard error instead.

## Check health endpoints

The public listener answers liveness; the operations listener answers readiness and metrics:

```bash
curl -fsS http://<api-host>:8080/healthz
curl -fsS http://<api-host>:9090/readyz
curl -fsS http://<api-host>:9090/metrics
```

Exact output:

```text
{"status":"ok"}
{"status":"ready"}
# HELP agent_marketplace_active_requests Active HTTP requests.
# TYPE agent_marketplace_active_requests gauge
agent_marketplace_active_requests{listener="operations"} 1
...
```

`/readyz` returns `503` when the recorded configuration authority does not match `management.mode`, the stored revocations do not match the process's configuration, the metadata or blob store does not respond, or the instance record is missing from the metadata store. The [health and metrics reference](../../reference/marketplace-health-and-metrics.md) describes each check.

Only processes that run the API (`--role api` or `--role all`) serve these endpoints. A `--role worker` process has no HTTP listener. Do not expose `/readyz` or `/metrics` through the public reverse proxy.

## Watch for problems in the logs

Read the log file or collect it with your container tooling. Alert or investigate when you see:

- `serve failed` with reason `open stores`: the process could not open storage, which includes a failed database migration.
- `reindex failed`, `process refresh job failed`, or `managed refresh attempt failed`: a registry refresh failed. The cause is in the `error` or `err` attribute; a managed attempt also carries `failure_code`, a stable classification that appears on the attempt in `/api/v1/admin/attempts`.
- `license policy rejected resource` and other records whose `code` attribute starts with `marketplace.policy.`: ingestion policy rejected a resource. See [How to manage ingestion policy](./manage-ingestion-policy.md) for the finding codes.
- `marketplace.ingest.duplicate-resource` in the `code` attribute: two paths in one registry publish the same kind and name; the first wins.
- `resource revision archive digest repaired`: a refresh staged unchanged content under a different archive digest. The stored digest was repaired and the previous artifact stays downloadable, but locks that reference the old digest fail `sync --locked` or `sync --check`. [How to reset and reindex a registry](./reset-and-reindex-a-registry.md) explains how consumers recover.
- `worker configuration authority rejected`: a process opened stores whose recorded authority mode conflicts with its `management.mode`, for example after adoption or a rollback.

## Alert on metrics

Scrape `/metrics` on the operations listener and alert against your service objectives: the request counter for error rate, the public latency histogram for user-facing latency (it also counts the `503` responses the authority and availability checks produce before routing), and the metadata pool counters for pool contention. The [health and metrics reference](../../reference/marketplace-health-and-metrics.md#metrics) lists every series and label. The server exposes no metrics for the worker, the job queue, or reindex runs, so watch those through the logs and your platform. The Helm chart can create a `ServiceMonitor` that scrapes the `operations` port (`serviceMonitor.enabled`).

## Related documentation

- [How to reset and reindex a registry](./reset-and-reindex-a-registry.md)
- [Marketplace health and metrics reference](../../reference/marketplace-health-and-metrics.md)
