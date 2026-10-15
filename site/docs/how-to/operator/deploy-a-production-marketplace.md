# How to deploy a production Marketplace

Run the hosted Marketplace with separate API and worker processes, external storage, and a private operations listener. Each supported packaging has its own guide. This page gives the order of steps.

## Prerequisites

- PostgreSQL and an S3-compatible bucket. SQLite and filesystem storage suit only a single all-in-one replica.
- A TLS-terminating reverse proxy or ingress that enforces authentication. The application has none.
- A secret store for the database DSN and any registry credentials.
- Container images at an immutable tag or digest: the API and worker images built from `Dockerfile.marketplace`, the Nuxt UI image, and the documentation-only VitePress image. Only the worker image contains Git, which acquisition needs.

## 1. Pick a packaging

| Packaging | Guide | Use when |
|---|---|---|
| Docker Compose (PostgreSQL + S3) | [`deploy/compose/postgres-s3/README.md`](https://github.com/Adyen/agent-marketplace/blob/main/deploy/compose/postgres-s3/README.md) | One host, and you manage the proxy yourself |
| Helm chart, `mode: split` | [`deploy/helm/agent-marketplace/README.md`](https://github.com/Adyen/agent-marketplace/blob/main/deploy/helm/agent-marketplace/README.md) | Kubernetes, with separate API and worker scaling |

Both packagings run `serve --role api` and `serve --role worker` as separate processes. `serve --role all` runs both in one process and is meant for local development and SQLite.

## 2. Write the operator configuration

```yaml
apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: team-marketplace
  name: Team Agent Marketplace
  canonical_url: https://marketplace.example.com
  browser_base: https://marketplace.example.com
listen: 0.0.0.0:8080
operations_listen: 0.0.0.0:9090   # private network only; never routed by the proxy
storage:
  metadata:
    driver: postgres
    dsn_file: /run/secrets/marketplace-postgres-dsn
  blobs:
    driver: s3
    bucket: marketplace-blobs
    region: eu-west-1
refresh:
  interval: 30m
refresh_concurrency: 2
policy:
  license_allowlist: [MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause]
  license_undeclared: reject
registries:
  - id: shared-agents
    slug: shared-agents
    origin: https://github.com/example-org/shared-agents.git
    ref: main
    roots:
      - path: .
        source_harness: claude
```

- Use `dsn_file` (a mounted secret) or `dsn_env`. Never commit a literal `dsn` to a file. Exactly one of the three must be set.
- `operations_listen` defaults to `127.0.0.1:9090`. Bind it to a non-loopback address only on a private network, as in the example, when probes or metrics scrapers run outside the container.
- Set `instance.id` once and never change it, because consumers verify it on every install.
- The S3 client gets credentials from the AWS SDK default chain (for example an IAM role or workload identity), not from this file. Set `storage.blobs.endpoint` for an S3-compatible service other than AWS.
- The [operator configuration reference](../../reference/marketplace-operator-configuration.md) lists every field.

## 3. Wire secrets and network

For the Helm chart, enable both browser-facing workloads before configuring routes:

```yaml
web:
  enabled: true
docs:
  enabled: true
```

- Mount the DSN file and any registry credential files read-only, readable by the container's non-root user (UID 1000). If a registry sets `ca_file`, mount it in every API and worker container, because every role validates the configuration at startup.
- Limit worker egress to the registry origins and the object store.
- Route `/.well-known/agent-marketplace.json`, `/api/`, and `/healthz` to the API; `/docs/` to the documentation image; and the remaining `/` traffic to the UI. See [How to configure a reverse proxy](./configure-a-reverse-proxy.md).

## 4. Roll out

1. Start the API replicas. Each process applies pending database migrations when it opens the stores, so when you upgrade across versions, start one API replica first and scale up after it is ready.
2. Start the worker. One replica is enough for most deployments. Workers lease refresh jobs through the metadata store, so extra replicas do not process the same job twice, but each one needs its own egress to the registry origins.
3. Point the proxy at the API, UI, and documentation services.

For upgrades, use a rolling update for the API and `Recreate` for the worker. The Helm chart README shows the values.

## 5. Verify

```bash
# through the proxy
curl -fsS https://marketplace.example.com/healthz
curl -fsS https://marketplace.example.com/.well-known/agent-marketplace.json
# from inside the private network
curl -fsS http://<api-host>:9090/readyz
```

Output (the well-known document is truncated):

```text
{"status":"ok"}
{"instance_id":"team-marketplace","version":"...","canonical_url":"https://marketplace.example.com","api_base":"/api/v1",...}
{"status":"ready"}
```

`/healthz` shows that the process is running. `/readyz` returns `{"status":"ready"}` only when the database's recorded configuration authority matches `management.mode`, the stored revocations match the process's configuration, the metadata store is readable, and the blob store responds. The [health and metrics reference](../../reference/marketplace-health-and-metrics.md#readyz) describes each check.

The worker queues a refresh for every registry when it starts. To run the first ingestion yourself and watch it finish, run this from the worker image:

```bash
agent-marketplace serve reindex --config /app/marketplace.yaml
```

Then confirm that a known resource is searchable with `agent-marketplace search <name> --marketplace https://marketplace.example.com`.

## Rollback

Startup migrates the database schema forward, and there is no automatic down-migration. Take a database backup before every upgrade (see [How to back up and restore a Marketplace](./back-up-and-restore.md)). To roll back, restore that backup and redeploy the previous image.

## Boundaries

- The public listener returns `404` for `/readyz` and `/metrics`.
- The application has no built-in authentication, authorization, or TLS. The proxy provides all three.
- `/metrics` reports HTTP request counters, a latency histogram, and in-flight requests from the API process only. The worker has no HTTP listener. Read [health and metrics](../../reference/marketplace-health-and-metrics.md) and [How to reindex and monitor a Marketplace](./reindex-and-monitor.md) before designing alerts.

## Related documentation

- [How to enable and use the Marketplace admin panel](./enable-and-use-the-admin-panel.md)
- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
- [Marketplace architecture](../../explanation/marketplace-architecture.md)
- [Marketplace trust model](../../explanation/marketplace-trust-model.md)
