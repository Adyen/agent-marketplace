# Run a Marketplace on Kubernetes

In this tutorial you deploy a Marketplace on a Kubernetes cluster with the repository's Helm chart, let it index one public registry, open the web UI in your browser, and install a resource from it with the `agent-marketplace` CLI.

The deployment runs one all-in-one Marketplace pod with SQLite on a persistent volume, plus the Nuxt web UI, both behind one ingress. At the end you delete the namespace and the throwaway checkout, so nothing on your machine or cluster survives the tutorial. [Deploy a production Marketplace](../how-to/operator/deploy-a-production-marketplace.md) shows the split mode with PostgreSQL and S3 that you grow into when you need more than one replica.

::: danger No authentication on the public routes
The Marketplace has no built-in authentication, authorization, or TLS. This tutorial uses a placeholder hostname that resolves only on your machine, but anyone who can reach your cluster's ingress and guesses that host header can still browse the deployment. Run the tutorial on a cluster you control, and put an authenticating TLS boundary in front of the public routes before the Marketplace matters; see [How to configure a reverse proxy](../how-to/operator/configure-a-reverse-proxy.md).
:::

## Before you begin

You need:

- `kubectl` connected to a Kubernetes cluster you control, and Helm 3. A local [kind](https://kind.sigs.k8s.io/) cluster works if you created it with the [ingress port mappings](https://kind.sigs.k8s.io/docs/user/ingress/) that publish port 80 on `127.0.0.1`.
- An ingress controller in the cluster. The examples use the class `nginx`; change `ingress.className` if yours differs.
- The `agent-marketplace` CLI on your `PATH` (see [Install](../install.md)).
- The latest release tag from the [releases page](https://github.com/Adyen/agent-marketplace/releases). Every release publishes the Marketplace `worker` and `api` images and the `web` image to GHCR; releases that predate image publishing have none.

Run all steps in the same terminal window so the variables stay set.

## 1. Check out the chart at your version

Set the Marketplace version and clone the repository at that tag. The clone provides the Helm chart at the same version as your images:

```bash
export MARKETPLACE_VERSION=v4.2.0   # replace with the latest release tag
TUTORIAL_ROOT=$(mktemp -d)
git clone --depth 1 --branch "$MARKETPLACE_VERSION" \
  https://github.com/Adyen/agent-marketplace.git "$TUTORIAL_ROOT/agent-marketplace"
cd "$TUTORIAL_ROOT/agent-marketplace"
```

## 2. Create the namespace

Keep the tutorial in its own namespace so the cleanup in step 11 removes everything at once:

```bash
kubectl create namespace marketplace-tutorial
```

## 3. Write the values file

The chart's defaults already run the all-in-one Marketplace with SQLite and a persistent volume claim. Create `values.yaml` in the checkout root that pins the images, enables the web UI, routes one hostname through the ingress, and adds the operator configuration with one registry:

```bash
cat > values.yaml <<EOF
image:
  repository: ghcr.io/adyen/agent-marketplace/worker
  tag: ${MARKETPLACE_VERSION}

web:
  enabled: true
  image:
    repository: ghcr.io/adyen/agent-marketplace/web
    tag: ${MARKETPLACE_VERSION}

persistence:
  enabled: true

config:
  content: |
    apiVersion: agent-marketplace.io/marketplace/v1
    instance:
      id: kubernetes-tutorial
      name: Kubernetes Tutorial Marketplace
      canonical_url: http://marketplace.example.com
      browser_base: http://marketplace.example.com
    listen: 0.0.0.0:8080
    operations_listen: 0.0.0.0:9090
    storage:
      metadata:
        driver: sqlite
        dsn: /var/lib/agent-marketplace/marketplace.db
      blobs:
        driver: filesystem
        path: /var/lib/agent-marketplace/blobs
    acquisition_default: mirror
    refresh:
      interval: 30m
    # SQLite is single-process in this example; keep refresh concurrency low.
    refresh_concurrency: 1
    policy:
      license_undeclared: allow
    registries:
      - id: skills
        slug: skills
        origin: https://github.com/anthropics/skills.git
        ref: main
        roots:
          - path: skills

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: marketplace.example.com
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
        - path: /
          pathType: Prefix
          service: web
          port: 3000
EOF
```

Four things in this file deserve a second look:

- The Marketplace image is the `worker` target. The all-in-one role runs acquisition, which needs Git, and only the worker image contains it.
- `marketplace.example.com` is a placeholder host. With the `/etc/hosts` entry from step 7, only your machine resolves it.
- The `registries` entry tells the Marketplace to clone `https://github.com/anthropics/skills.git` at `main` and scan its `skills` directory. `license_undeclared: allow` accepts resources that declare no license; without it, the Marketplace rejects them.
- The ingress routes the well-known document, `/api/`, and `/healthz` to the API, and `/` to the web UI at the origin root. The web UI calls the API at the same origin, which is why both live behind one host.

::: details Private GHCR packages
If your organization keeps its GHCR packages private, create a pull secret and add it to `values.yaml` under a top-level `imagePullSecrets` key:

```bash
kubectl create secret docker-registry ghcr-pull \
  --namespace marketplace-tutorial \
  --docker-server=ghcr.io \
  --docker-username=<github-username> \
  --docker-password=<token with read:packages>
```

```yaml
imagePullSecrets:
  - name: ghcr-pull
```
:::

## 4. Render and review

Lint the chart with your values and render the full manifest before applying it:

```bash
helm lint --strict deploy/helm/agent-marketplace -f values.yaml
helm template marketplace deploy/helm/agent-marketplace \
  --namespace marketplace-tutorial \
  -f values.yaml > rendered.yaml
```

`helm lint` passes with `1 chart(s) linted, 0 chart(s) failed`, and `rendered.yaml` holds eight objects: two Deployments (`marketplace-agent-marketplace-all` and `marketplace-agent-marketplace-web`), two Services, one Ingress, one ConfigMap, one ServiceAccount, and one PersistentVolumeClaim. Confirm the count:

```bash
grep -c '^kind:' rendered.yaml
```

Expected output:

```text
8
```

## 5. Install the release

```bash
helm upgrade --install marketplace deploy/helm/agent-marketplace \
  --namespace marketplace-tutorial \
  -f values.yaml
```

Helm reports `STATUS: deployed` for the `marketplace` release in the `marketplace-tutorial` namespace.

## 6. Watch the rollout

```bash
kubectl get deployments,pods,pvc -n marketplace-tutorial -w
```

Wait until `marketplace-agent-marketplace-all` and `marketplace-agent-marketplace-web` show `1/1` ready and the persistent volume claim shows `Bound`, then stop the command with `Ctrl-C`.

Readiness is not just the process starting: the chart probes `/readyz` on the private operations port (9090) for readiness and startup, and `/healthz` on the application port for liveness. `/readyz` returns ready only when the database and blob stores answer.

## 7. Point your machine at the ingress

```bash
kubectl get ingress -n marketplace-tutorial
```

Copy the `ADDRESS` of the `marketplace-agent-marketplace` ingress and map the tutorial host to it. On a local kind cluster the address is `127.0.0.1`, because the ingress is published on localhost:

```bash
echo "<address> marketplace.example.com" | sudo tee -a /etc/hosts
```

## 8. Verify the Marketplace

Check the public health endpoint and the well-known document:

```bash
curl -fsS http://marketplace.example.com/healthz
curl -fsS http://marketplace.example.com/.well-known/agent-marketplace.json
```

Expected output (the `version` field reflects your release):

```json
{"status":"ok"}
{"instance_id":"kubernetes-tutorial","version":"v4.2.0","canonical_url":"http://marketplace.example.com","api_base":"/api/v1","browser_base":"http://marketplace.example.com","browser_latest_route_template":"http://marketplace.example.com/r/{registry}/{kind}/{name}","browser_exact_route_template":"http://marketplace.example.com/r/{registry}/{kind}/{name}/revisions/{revision}","api_versions":["v1"],"artifact_schema_versions":["v1"],"resource_kinds":["skill","command","subagent","hook","mcpserver","collection"],"acquisition_modes":["index-only","mirror"],"features":["inspection"]}
```

The all-in-one pod started indexing the `skills` registry when it became ready. Poll the search endpoint until the `frontend-design` skill is published:

```bash
until curl -fsS "http://marketplace.example.com/api/v1/search?q=frontend-design" \
  | grep -q frontend-design; do sleep 10; done
curl -fsS "http://marketplace.example.com/api/v1/search?q=frontend-design"
```

Expected output (trimmed; the revision digests and score are omitted):

```json
{"items":[{"registry_id":"skills","registry_slug":"skills","kind":"skill","name":"frontend-design","description":"Guidance for distinctive, intentional visual design when building new UI or reshaping an existing one. Helps with aesthetic direction, typography, and making choices that don't read as templated defaults.","availability":"available","metadata":{"license":"Apache-2.0"}}],"limit":100,"offset":0,"total":1}
```

If the loop keeps running for several minutes, check the pod logs for acquisition or indexing errors:

```bash
kubectl logs -n marketplace-tutorial deployment/marketplace-agent-marketplace-all
```

## 9. Open the web UI

Open `http://marketplace.example.com/` in your browser. The catalog lists every published skill from the `skills` registry, each with its name, description, license, and a **Copy install command** button.

## 10. Install a resource with the CLI

The consumer side of the tutorial also runs inside the throwaway clone. Replace the clone's `.agents/config.yaml` with one that selects `claude` as the target harness, so installed skills are written to `.claude/skills/`:

```bash
cat > .agents/config.yaml <<'EOF'
apiVersion: agent-marketplace/v4
harnesses:
  - claude
EOF
```

Register the tutorial Marketplace in the clone's repository configuration, then install the skill by its full resource URL:

```bash
agent-marketplace install http://marketplace.example.com --scope repo
agent-marketplace install http://marketplace.example.com/r/skills/skill/frontend-design
```

The second command fetches the latest usable revision of the skill, prints a verified preview, and stops before installing, because the preview reports a security finding:

```text
Warning: 1 security finding
  WARN  [frontend-design] remote reference: http://
Active capabilities: skill

Nothing has been installed.
Error: marketplace install requires risk acceptance: 1 security finding(s); re-run with --accept-risk (or --yes)
```

The skill's instructions reference remote `http://` addresses, and the CLI never installs a resource with findings until you have read them and accepted the risk. You can inspect the skill's contents in the web UI from step 9. When you have read the preview, confirm the installation:

```bash
agent-marketplace install http://marketplace.example.com/r/skills/skill/frontend-design --accept-risk
```

Expected output:

```text
Installing frontend-design · skill

Source: skills

✓ Installed frontend-design
```

The repository configuration now records the Marketplace endpoint and pins the exact revision of the skill by its digest. Check the result:

```bash
agent-marketplace status
agent-marketplace sync --check
ls .claude/skills/frontend-design/SKILL.md
```

`status` lists the `http://marketplace.example.com` endpoint and the installed skill, `sync --check` exits with status `0` because the files on disk match the configuration, and `ls` prints the path of the skill's `SKILL.md`.

## 11. Clean up

Uninstall the release, delete the namespace, leave the checkout, and remove the temporary directory:

```bash
helm uninstall marketplace --namespace marketplace-tutorial
kubectl delete namespace marketplace-tutorial
cd -
rm -rf "$TUTORIAL_ROOT"
```

Then remove the `marketplace.example.com` line you added to `/etc/hosts`.

## What happened

The Helm chart rendered a Marketplace deployment you could grow into a production one:

- One all-in-one Deployment runs the API and the refresh worker in a single process, with SQLite and mirrored blobs on the persistent volume claim.
- One web Deployment serves the Nuxt catalog UI at the origin root and reaches the API through the cluster-internal Service.
- The API Service exposes the public application port 8080 and a private operations port 9090. The Ingress routes only the intended public paths; readiness and metrics stay on the operations port, off the Ingress.
- The ConfigMap holds the operator configuration. When you change `config.content`, the chart changes a `checksum/config` annotation and Kubernetes restarts the Marketplace pods with the new configuration.
- The all-in-one pod indexed the `skills` registry when it started and refreshes it every 30 minutes.

## Next steps

- [Deploy a production Marketplace](../how-to/operator/deploy-a-production-marketplace.md) — split API and worker Deployments, PostgreSQL and S3, rolling updates, and the trust boundary.
- [How to configure a reverse proxy](../how-to/operator/configure-a-reverse-proxy.md) — the public path list, TLS, and authentication for a real boundary.
- [How to reindex and monitor a Marketplace](../how-to/operator/reindex-and-monitor.md) — what `/readyz` and `/metrics` report before you alert on them.
- [How to enable and use the admin panel](../how-to/operator/enable-and-use-the-admin-panel.md) — manage registries from the browser with single sign-on.
- The [Helm chart README](https://github.com/Adyen/agent-marketplace/blob/main/deploy/helm/agent-marketplace/README.md) — every chart value, including split mode, existing ConfigMaps, ServiceMonitor, and NetworkPolicy.
