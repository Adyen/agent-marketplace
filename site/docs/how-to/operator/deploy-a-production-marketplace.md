# How to deploy a production Marketplace

Run the hosted Marketplace with separate API and worker processes, external storage, and a private operations listener. This page gives the sequence and the decisions; the two supported packagings each have their own detailed guide.

## Prerequisites

- PostgreSQL and an S3-compatible bucket. SQLite/filesystem storage is single-process and only suitable for one all-in-one replica.
- A TLS-terminating reverse proxy or ingress that enforces authentication. The application has none.
- A secret store for the database DSN and any registry credentials.
- Container images for the API/worker binary (and the optional Nuxt UI and VitePress site) at an immutable tag or digest.

## 1. Pick a packaging

| Packaging | Guide | Use when |
|---|---|---|
| Docker Compose (PostgreSQL + S3) | [`deploy/compose/postgres-s3/README.md`](https://github.com/Adyen/agent-marketplace/blob/main/deploy/compose/postgres-s3/README.md) | One host, you manage the proxy yourself |
| Helm chart, `mode: split` | [`deploy/helm/agent-marketplace/README.md`](https://github.com/Adyen/agent-marketplace/blob/main/deploy/helm/agent-marketplace/README.md) | Kubernetes, separate API and worker scaling |

Both packagings run `serve --role api` and `serve --role worker` as separate processes. `serve --role all` is for local development and SQLite only.

## 2. Write the operator configuration

```yaml
apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: team-marketplace
  name: Team Agent Marketplace
  canonical_url: https://marketplace.example.com
  browser_base: https://marketplace.example.com/marketplace
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

- Use `dsn_file` (mounted secret) or `dsn_env`; never a literal `dsn` in a committed file.
- Set `instance.id` once and never change it: consumers verify it on every install.
- S3 credentials come from the AWS SDK default chain (IAM role or workload identity), not from this file.
- The full field list is in the [operator configuration reference](../../reference/marketplace-operator-configuration.md).

## 3. Wire secrets and network

- Mount the DSN and any registry `credentials` files read-only, owned by the container's non-root UID.
- Restrict worker egress to the registry origins and the object store.
- Expose only `/.well-known/agent-marketplace.json`, `/api/`, `/healthz`, and the UI path through the proxy. See [How to configure a reverse proxy](./configure-a-reverse-proxy.md).

## 4. Roll out

1. Start the API replicas. Startup runs database migrations, so start one API replica first when upgrading across versions, then scale.
2. Start the worker. One replica is enough for most deployments; refresh jobs are leased through the metadata store, so additional worker replicas do not double-process a registry, but each extra replica needs its own egress to the registry origins.
3. Point the proxy at the API and UI.

For upgrades, use a rolling strategy for the API and `Recreate` for the worker (the Helm chart's README shows the values).

### Backfill v1 artifact proofs before changing Go toolchains

Before upgrading a Marketplace built with Go 1.26 to Go 1.27 or later, deploy this proof-capable release built with Go 1.26.6. Stop the worker so the artifact set is stable, then run that binary against the production stores:

```bash
agent-marketplace serve backfill-artifact-proofs \
  --config /app/marketplace.yaml \
  --limit 100
```

The command processes one bounded page and prints JSON. While `remaining` is `true`, rerun it with `--after` set to the returned `next_after`. Existing valid proofs are checked and counted without being replaced, so retrying a page is safe. A compressor mismatch fails closed with an error requiring the original Go compression toolchain. Keep the worker stopped until every page reports `remaining: false`; the API may remain available because the operation only adds immutable proof blobs.

Back up metadata and blobs together after the backfill. You may then deploy API and consumer binaries built with Go 1.27 or later. Keep workers that publish v1 artifacts on Go 1.26.6 until a compressor-independent publication protocol is deployed; otherwise unchanged logical content can produce a different `archive_digest` and conflict with immutable revision metadata. New publications retain both the legacy final archive and its proof automatically, so plan blob capacity for roughly twice the compressed artifact storage.

## 5. Verify

```bash
# through the proxy
curl -fsS https://marketplace.example.com/healthz
curl -fsS https://marketplace.example.com/.well-known/agent-marketplace.json
# from inside the private network
curl -fsS http://<api-host>:9090/readyz
```

`/healthz` proves the process is up; `/readyz` proves storage is reachable. Then trigger and observe the first ingestion:

```bash
agent-marketplace serve reindex --config /app/marketplace.yaml
```

and confirm a known resource is searchable with `agent-marketplace search <name> --marketplace https://marketplace.example.com`.

## Rollback

The database schema is migrated forward on startup and there is no automatic down-migration. Take a database backup before every upgrade (see [How to back up and restore a Marketplace](./back-up-and-restore.md)); roll back by restoring it and redeploying the previous image.

## Boundaries

- The public listener returns `404` for `/readyz` and `/metrics`.
- No built-in authentication, authorization, or TLS; the proxy owns all three.
- `/metrics` exposes two unlabeled series; see [health and metrics](../../reference/marketplace-health-and-metrics.md) before designing alerts.

## Related documentation

- [How to enable and use the Marketplace admin panel](./enable-and-use-the-admin-panel.md)
- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
- [Marketplace architecture](../../explanation/marketplace-architecture.md)
- [Marketplace trust model](../../explanation/marketplace-trust-model.md)
