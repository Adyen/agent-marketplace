# Deploy Agent Marketplace on Kubernetes

This guide shows how to deploy the Agent Marketplace service with the reusable Helm chart in this directory. The chart defaults to one all-in-one Deployment, with optional web and documentation frontends. Use split mode for external shared storage and separate API and worker scaling.

## Prerequisites

Before starting, ensure:

- Helm 3 is available.
- Container images for the API/worker binary and any enabled frontend are available to the cluster.
- A Marketplace operator configuration is prepared, or the default example configuration is suitable for a local SQLite/filesystem deployment.
- A private authentication and TLS boundary is available in front of public routes. The application does not provide authentication or authorization.

## 1. Choose the storage topology

For a single-process or development deployment, use the default SQLite/filesystem configuration and keep one replica:

```yaml
mode: all
persistence:
  enabled: true
all:
  replicas: 1
```

For a multi-replica deployment, use external PostgreSQL and object storage in `config.content`, and provide the database DSN through an existing Secret:

```yaml
mode: split
persistence:
  enabled: false
storage:
  dsnSecret:
    name: marketplace-database
    key: dsn
    envName: MARKETPLACE_POSTGRES_DSN
config:
  content: |
    apiVersion: agent-marketplace.io/marketplace/v1
    instance:
      id: example-marketplace
      name: Example Agent Marketplace
      canonical_url: https://marketplace.example.invalid
    listen: 0.0.0.0:8080
    operations_listen: 0.0.0.0:9090
    storage:
      metadata:
        driver: postgres
        dsn_env: MARKETPLACE_POSTGRES_DSN
      blobs:
        driver: s3
        bucket: replace-with-bucket
        region: replace-with-region
        endpoint: https://object-storage.example.invalid
    registries: []
```

Do not put a literal DSN or other credentials in `values.yaml`. Use a Secret manager or an existing Kubernetes Secret.

## 2. Configure images and optional frontends

Set the image repositories and immutable tags or digests supplied by your release pipeline:

```yaml
image:
  repository: agent-marketplace
  tag: "1.0.0"
  digest: ""

web:
  enabled: true
  image:
    repository: agent-marketplace-web
    tag: "1.0.0"

site:
  enabled: true
  image:
    repository: agent-marketplace-site
    tag: "1.0.0"
```

The web container receives an internal API base automatically. The API Service exposes the public `http` port and a named `operations` port (`service.api.operationsPort`, default 9090). The operations port is for private monitoring and health endpoints only; it is not reachable through the Ingress.

## 3. Configure the ingress boundary

Supply the hostname, TLS secret, ingress class, and authentication annotations for the ingress or gateway used by your cluster. The chart does not invent these values:

```yaml
ingress:
  enabled: true
  className: example-ingress
  tls:
    - secretName: marketplace-tls
      hosts:
        - marketplace.example.invalid
  hosts:
    - host: marketplace.example.invalid
      paths:
        - path: /.well-known/agent-marketplace.json
          pathType: Exact
          service: api
          port: 8080
        - path: /api/
          pathType: Prefix
          service: api
          port: 8080
        - path: /healthz
          pathType: Exact
          service: api
          port: 8080
        - path: /marketplace/
          pathType: Prefix
          service: web
          port: 3000
        - path: /
          pathType: Prefix
          service: site
          port: 8080
```

Do not route `/readyz` or `/metrics` through the public ingress. If a monitoring system needs `/metrics`, enable `serviceMonitor` or configure an equivalent private monitoring path. The rendered ServiceMonitor is named after the API component (for example `agent-marketplace-api` when `fullnameOverride: agent-marketplace` is set) and scrapes the named `operations` Service port.

## 4. Tune workload behavior per component

Each workload block (`api`, `worker`, `all`, `web`, `site`) accepts the same optional keys:

- `strategy`: the Deployment `spec.strategy`. For example, give the API a zero-unavailable rolling update while the singleton worker uses `Recreate`:
  ```yaml
  api:
    strategy:
      type: RollingUpdate
      rollingUpdate:
        maxSurge: 1
        maxUnavailable: 0
  worker:
    strategy:
      type: Recreate
  ```
- `podSecurityContext`: merged over the global `podSecurityContext` for that component's pods only. Use it for component-specific needs such as a frontend UID without imposing it on other images:
  ```yaml
  web:
    podSecurityContext:
      runAsUser: 1000
      runAsGroup: 1000
  ```
- `extraVolumes` / `extraVolumeMounts`: appended after the global `extraVolumes`/`extraVolumeMounts` for that component only. Use them to attach a CA bundle Secret to just the API and worker:
  ```yaml
  api:
    extraVolumes:
      - name: custom-ca
        secret:
          secretName: custom-ca
    extraVolumeMounts:
      - name: custom-ca
        mountPath: /etc/ssl/custom-ca
        readOnly: true
  ```

The API and `all` roles also render an optional `startupProbe` (`<role>.probes.startup`, enabled by default) that polls `/readyz` on the `operations` port so a slow-starting API is not killed by its liveness probe. The worker has no HTTP listener and renders no HTTP probes.

When the chart manages the ConfigMap (`config.enabled: true` and no `config.existingConfigMap`), the API, worker, and `all` pods carry a `checksum/config` pod annotation computed from the rendered ConfigMap. Changing `config.content` therefore rolls those pods automatically. Set `config.nameOverride` to pin the ConfigMap name, and `config.key` to change both the data key and the mounted `subPath`.

## 5. Render and apply the chart

Write the selected values to a local file and render before applying:

```bash
helm lint deploy/helm/agent-marketplace -f values.yaml
helm template marketplace deploy/helm/agent-marketplace \
  --namespace marketplace \
  -f values.yaml > rendered.yaml
```

Check the rendered output for the expected Deployments, Services, probes, and storage objects. Apply it through the deployment system used by your cluster:

```bash
helm upgrade --install marketplace deploy/helm/agent-marketplace \
  --namespace marketplace \
  --create-namespace \
  -f values.yaml
```

## 6. Verify the deployment

Verify that API readiness and frontend routes are reachable from the appropriate private or authenticated paths:

```bash
kubectl get deployments,services,pods -n marketplace
kubectl describe deployment marketplace-agent-marketplace-api -n marketplace
```

The API readiness and startup probes should use `/readyz` on the operations port. The API Service should expose the application HTTP port plus the named `operations` port for private monitoring; the Ingress still exposes only the intended public routes. Worker pods do not have an HTTP listener; observe their process state and logs instead of adding an HTTP probe.

## Troubleshooting

- **API pods fail before becoming ready**: inspect the mounted operator configuration and confirm the configured database/blob drivers and Secret references match.
- **Worker pods restart**: check outbound access to configured registry origins and object storage, then inspect the worker logs for acquisition or indexing errors.
- **The web UI cannot reach the API**: confirm the API Service is enabled and that the web deployment can resolve the generated API Service name.
- **Metrics are unavailable**: keep the operations listener private and configure `serviceMonitor.enabled: true` or a private monitoring route; do not add `/metrics` to the public ingress.
