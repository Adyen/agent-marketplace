# How to deploy a production Marketplace

Deploy the hosted Marketplace in the recommended multi-process topology.

## Prerequisites

- A container platform.
- A PostgreSQL or SQLite database and an S3 or filesystem blob store.
- A TLS-terminating reverse proxy.

## 1. Choose the topology

Run at least two processes:

- `serve --role api`: serves the public HTTP routes.
- `serve --role worker`: performs ingestion and indexing.

`serve --role all` is only for local development.

## 2. Write operator configuration

Create `marketplace.yaml`:

```yaml
apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: team-marketplace
  name: Team Agent Marketplace
  canonical_url: https://marketplace.example.com
  browser_base: https://marketplace.example.com/marketplace
listen: 0.0.0.0:8080
operations_listen: 127.0.0.1:9090
storage:
  metadata:
    driver: postgres
    dsn_env: POSTGRES_DSN
  blobs:
    driver: s3
    bucket: marketplace-blobs
    region: us-east-1
registries:
  - id: shared-agents
    slug: shared-agents
    origin: https://github.com/example-org/shared-agents.git
    ref: main
    roots:
      - path: .
```

## 3. Mount secrets

Provide the DSN through a secret. Do not commit literal credentials.

## 4. Start the services

```bash
agent-marketplace serve --config /app/marketplace.yaml --role api
agent-marketplace serve --config /app/marketplace.yaml --role worker
```

## 5. Verify readiness

```bash
curl -fs https://marketplace.example.com/healthz
curl -fs http://localhost:9090/readyz
```

## Boundaries

- The public listener returns `404` for `/readyz` and `/metrics`.
- The operations listener must not be exposed through the reverse proxy.
- No built-in authentication is provided; the reverse proxy must enforce it.

## Related documentation

- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
- [How to configure a reverse proxy](./configure-a-reverse-proxy.md)
