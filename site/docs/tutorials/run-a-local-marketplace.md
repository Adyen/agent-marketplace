# Run a local Marketplace

In this tutorial, you will run a single-process `agent-marketplace` Marketplace on your machine using Docker and the repository's root `compose.yaml`.

## Before you begin

You need:

- Docker and `docker compose`.
- `git`.
- The `agent-marketplace` repository checked out.
- `agent-marketplace` on your `PATH`.

Run every command from the repository root. The tutorial uses `TUTORIAL_ROOT` as a temporary directory that it removes at the end.

## 1. Make a throwaway checkout

Copy the repository to a temporary directory so the bundled example config is not modified in your real checkout:

```bash
TUTORIAL_ROOT=$(mktemp -d)
git clone . "$TUTORIAL_ROOT/agent-marketplace"
cd "$TUTORIAL_ROOT/agent-marketplace"
```

## 2. Configure the local Marketplace

Replace the example Marketplace config with one that uses loopback URLs and a public registry:

```bash
cat > deploy/compose/sqlite-filesystem/config.yaml <<'EOF'
apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: local
  name: Local Agent Marketplace
  canonical_url: http://localhost:8080
  browser_base: http://localhost:8080/marketplace
listen: 0.0.0.0:8080
operations_listen: 127.0.0.1:9090
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
  hidden_content: warn
  license_undeclared: allow
  limits:
    repository_bytes: 104857600
    file_count: 1024
    file_bytes: 2097152
    artifact_bytes: 16777216
    clone_duration: 300s
    ingestion_duration: 120s
registries:
  - id: skills
    slug: skills
    origin: https://github.com/anthropics/skills.git
    ref: main
    roots:
      - path: skills
EOF
```

## 3. Configure the consumer

Before installing from the Marketplace, create a throwaway `.agents/config.yaml` that selects `claude` as the target harness:

```bash
mkdir -p .agents
cat > .agents/config.yaml <<'EOF'
apiVersion: agent-marketplace/v2
harnesses:
  - claude
EOF
```

## 4. Start the services

From the throwaway checkout root:

```bash
docker compose up --build -d
```

Expected: `docker compose ps` shows the `app`, `web`, `site`, and `proxy` services as `Up` (healthy) after a short startup.

## 5. Wait for health

Wait for the API to respond with the liveness body:

```bash
until curl -fs http://localhost:8080/healthz; do sleep 5; done
```

Expected:

```json
{"status":"ok"}
```

## 6. Poll for indexing

The first refresh ingests the public registry. Poll until the search returns the resource:

```bash
until agent-marketplace search frontend-design --marketplace http://localhost:8080 | grep -q frontend-design; do
  sleep 10
done
```

Representative output:

```text
MARKETPLACE      SOURCE  KIND   NAME             DESCRIPTION
http://local...  skills  skill  frontend-design  Frontend design guidance
```

## 7. Install one exact resource

Register the local Marketplace in the throwaway repo and install the exact resource by its full URL:

```bash
agent-marketplace install http://localhost:8080 --scope repo
agent-marketplace install http://localhost:8080/r/skills/skill/frontend-design
```

The first command records the Marketplace endpoint; the second fetches the latest usable revision and pins its exact digest. The CLI prints a verified preview before writing the pin. The `instance.id` from the server config is not automatically your Marketplace alias.

Expected: `agent-marketplace status` shows the `http://localhost:8080` endpoint; `agent-marketplace sync --check` exits `0`; and `.claude/skills/frontend-design/SKILL.md` is present.

## 8. Verify the installation

Check that the resource is in your configuration and that `sync` is healthy:

```bash
agent-marketplace status
agent-marketplace sync --check
```

Verify the skill is in your configured harness directory:

```bash
ls .claude/skills/frontend-design/SKILL.md
```

## 9. Clean up

Stop the containers, leave the cloned repository, and remove the throwaway directory:

```bash
docker compose down
cd -
rm -rf "$TUTORIAL_ROOT"
```

## What happened

The repository's `compose.yaml` started the Marketplace API, Nuxt UI, VitePress landing site, and Nginx reverse proxy. You configured a loopback Marketplace, allowed it to mirror a public registry, and installed one exact skill.

## Next steps

- [How to deploy a production Marketplace](../how-to/operator/deploy-a-production-marketplace.md)
- [Marketplace operator configuration reference](../reference/marketplace-operator-configuration.md)
