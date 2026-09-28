# Deploy Agent Marketplace on Kubernetes

This guide deploys the Agent Marketplace service with the reusable Helm chart in this directory. By default the chart renders one all-in-one Deployment (`mode: all`), and it can add the web and documentation frontends. Use `mode: split` when you have external shared storage and want to scale the API and worker separately.

## Prerequisites

Before starting, check that:

- Helm 3 is available.
- The cluster can pull the Marketplace image and the image for each frontend you enable. `Dockerfile.marketplace` builds two targets: `api`, and `worker`, which adds Git. The `all` and `worker` roles run acquisition and need an image built from the `worker` target. The top-level `image` applies to every Marketplace role; in split mode you can point `api.image` at an image built from the `api` target.
- A Marketplace operator configuration is ready, or the default example configuration is enough for a local SQLite/filesystem deployment.
- A private authentication and TLS boundary is available in front of the public routes. The public application routes have no authentication or authorization.

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

Do not put a literal DSN or other credentials in `values.yaml`. Use a secret manager or an existing Kubernetes Secret.

The chart's container ports are fixed at 8080 (`http`) and 9090 (`operations`), so keep `listen` and `operations_listen` on those ports. `operations_listen` must bind an address the kubelet can reach, such as `0.0.0.0:9090`, because the probes call `/readyz` on it. Keep that port off the Ingress.

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

The chart sets the web container's internal API base (`NUXT_API_BASE_INTERNAL`) to the API Service. The API Service exists in both modes; in `mode: all` it selects the all-in-one pods. It exposes the public `http` port and a named `operations` port (`service.api.operationsPort`, default 9090). The operations port is for private monitoring and health endpoints only, and the Ingress does not route to it.

## 3. Configure the ingress boundary

Supply the hostname, TLS secret, ingress class, and authentication annotations for the ingress or gateway your cluster uses. The chart has no defaults for these values:

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

With these paths, a request for exactly `/marketplace` (no trailing slash) falls through to the site service. The [reverse proxy contract](../../../docs/marketplace-reverse-proxy.md) requires a redirect from `/marketplace` to `/marketplace/`. An Ingress path cannot redirect by itself, so add that redirect with your ingress controller's or gateway's own mechanism.

Do not route `/readyz` or `/metrics` through the public ingress. If a monitoring system needs `/metrics`, enable `serviceMonitor` or configure an equivalent private monitoring path. The rendered ServiceMonitor is named after the API component (for example `agent-marketplace-api` when `fullnameOverride: agent-marketplace` is set). It selects the API Service and scrapes its named `operations` port, so it covers the all-in-one pods in `mode: all` and the API pods in split mode. Worker pods have no metrics listener.

To limit which pods can reach the API, all-in-one, web, and site pods, set `networkPolicy.enabled: true`. The policy allows traffic from the same namespace (`networkPolicy.allowSameNamespace`, default `true`) and from each namespace in `networkPolicy.ingressNamespaces`.

## 4. Tune workload behavior per component

Each workload block (`api`, `worker`, `all`, `web`, `site`) accepts the same optional keys:

- `strategy`: the Deployment `spec.strategy`. For example, give the API a rolling update with zero unavailable pods while the single worker uses `Recreate`:
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
- `extraVolumes` / `extraVolumeMounts`: appended after the global `extraVolumes`/`extraVolumeMounts` for that component only. Use them to attach a CA bundle Secret to only the API and worker:
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

The API and `all` roles also render a `startupProbe` (`<role>.probes.startup`, enabled by default). It polls `/readyz` on the `operations` port so the liveness probe does not kill an API that starts slowly. The API and `all` roles check liveness with `/healthz` on the `http` port and readiness with `/readyz` on the `operations` port. The worker has no HTTP listener, and its probes are disabled by default.

When the chart manages the ConfigMap (`config.enabled: true` and no `config.existingConfigMap`), the API, worker, and `all` pods carry a `checksum/config` annotation computed from the rendered ConfigMap. A change to `config.content` therefore restarts those pods. Set `config.nameOverride` to pin the ConfigMap name, and `config.key` to change both the data key and the mounted `subPath`. The chart mounts the configuration at `config.mountPath` (default `/etc/agent-marketplace/config.yaml`), which is the path the default `args` pass to `--config`.

The API reads operator configuration such as revocations when it starts, so a change takes effect only after every API pod restarts with the new configuration; a reindex does not apply it. The checksum annotation handles the restart for a chart-managed ConfigMap. With `config.existingConfigMap`, restart the pods yourself, for example with `kubectl rollout restart`.

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

Verify that API readiness and the frontend routes are reachable from the appropriate private or authenticated paths:

```bash
kubectl get deployments,services,pods -n marketplace
kubectl describe deployment marketplace-agent-marketplace-api -n marketplace
```

The Deployment name ends in the role: `-api` and `-worker` in split mode, and `-all` in the default `mode: all` (for example `marketplace-agent-marketplace-all`).

The API readiness and startup probes should use `/readyz` on the operations port. The API Service should expose the application HTTP port plus the named `operations` port for private monitoring, and the Ingress should expose only the intended public routes. Worker pods have no HTTP listener, so check their process state and logs instead of adding an HTTP probe.

## Troubleshooting

- If API pods fail before becoming ready, inspect the mounted operator configuration and confirm that the configured database and blob drivers match the Secret references.
- If worker pods restart, check outbound access to the configured registry origins and object storage, then look in the worker logs for acquisition or indexing errors.
- If acquisition cannot run Git, the pod is probably using an image built from the `api` target. The `worker` and `all` roles need an image built from the `worker` target.
- If the web UI cannot reach the API, confirm that the API Service is rendered and that the web Deployment can resolve the generated API Service name.
- If metrics are unavailable, keep the operations listener private and set `serviceMonitor.enabled: true`, or configure a private monitoring route. Do not add `/metrics` to the public ingress.
