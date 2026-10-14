# Run a local Marketplace

In this tutorial you run a complete Marketplace on your machine with Docker and the repository's root `compose.yaml`, let it index one public registry, and install a skill from it with the `agent-marketplace` CLI.

You work in a throwaway copy of the repository with its own Compose project, so your real checkout and any other local Marketplace stay untouched.

## Before you begin

You need:

- Docker with `docker compose`.
- `git` and `curl`.
- The `agent-marketplace` repository checked out.
- `agent-marketplace` on your `PATH` (see [Install](../install.md)).
- Port `8080` free on your machine.

Start in the root of your repository checkout. The tutorial stores everything in a temporary directory, `TUTORIAL_ROOT`, and removes it at the end. Run all steps in the same terminal window so the `TUTORIAL_ROOT` variable stays set.

## 1. Make a throwaway checkout

Clone the repository into a temporary directory and move into the clone:

```bash
TUTORIAL_ROOT=$(mktemp -d)
git clone . "$TUTORIAL_ROOT/agent-marketplace"
cd "$TUTORIAL_ROOT/agent-marketplace"
```

Give the clone its own Compose project name, so its containers and data volume stay separate from any other local Marketplace built from this repository:

```bash
echo "COMPOSE_PROJECT_NAME=marketplace-local-tutorial" > .env
```

## 2. Configure the local Marketplace

`compose.yaml` mounts `deploy/compose/sqlite-filesystem/config.yaml` into the app container as the operator configuration. Replace it in the clone with a configuration that uses `http://localhost:8080` URLs and indexes one public registry:

```bash
cat > deploy/compose/sqlite-filesystem/config.yaml <<'EOF'
apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: local
  name: Local Agent Marketplace
  canonical_url: http://localhost:8080
  browser_base: http://localhost:8080
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

The `registries` entry tells the Marketplace to clone `https://github.com/anthropics/skills.git` at `main` and scan its `skills` directory. `acquisition_default: mirror` makes the Marketplace keep its own copy of each published resource. `license_undeclared: allow` accepts resources that declare no license; without it, the Marketplace rejects them.

## 3. Configure the consumer

The CLI side of the tutorial also runs inside the clone. Replace the clone's `.agents/config.yaml` with one that selects `claude` as the target harness, so installed skills are written to `.claude/skills/`:

```bash
mkdir -p .agents
cat > .agents/config.yaml <<'EOF'
apiVersion: agent-marketplace/v4
harnesses:
  - claude
EOF
```

## 4. Start the services

Build the images and start the services in the background:

```bash
docker compose up --build -d
```

The first build takes a few minutes. Check the services with:

```bash
docker compose ps
```

After a short startup, `app`, `web`, and `docs` show as running and healthy, and `proxy` shows as running. The proxy has no health check of its own; it starts after the other three are healthy.

## 5. Wait for the API

Poll the public health endpoint until the API answers:

```bash
until curl -fs http://localhost:8080/healthz; do sleep 5; done
```

Expected output:

```json
{"status":"ok"}
```

## 6. Wait for indexing

When the app starts, it clones the registry and indexes it. Poll the Marketplace with `agent-marketplace search` until the `frontend-design` skill appears:

```bash
until agent-marketplace search frontend-design --marketplace http://localhost:8080 | grep -q frontend-design; do
  sleep 10
done
agent-marketplace search frontend-design --marketplace http://localhost:8080
```

Representative output (the description comes from the skill and may differ):

```text
MARKETPLACE            SOURCE  KIND   NAME             DESCRIPTION
http://localhost:8080  skills  skill  frontend-design  Frontend design guidance
```

The `SOURCE` column shows the registry slug, `skills`, from step 2. If the loop keeps running for several minutes, check the app logs with `docker compose logs app`.

## 7. Install one exact resource

Register the local Marketplace in the clone's repository configuration, then install the skill by its full resource URL:

```bash
agent-marketplace install http://localhost:8080 --scope repo
agent-marketplace install http://localhost:8080/r/skills/skill/frontend-design
```

The first command records the Marketplace endpoint in `.agents/config.yaml`. The second command fetches the latest usable revision of the skill, prints a verified preview, and then pins that exact revision by its digest. The server's `instance.id` (`local`) does not become a Marketplace alias in your configuration.

## 8. Verify the installation

Check the configuration and the files on disk:

```bash
agent-marketplace status
agent-marketplace sync --check
ls .claude/skills/frontend-design/SKILL.md
```

`status` lists the `http://localhost:8080` endpoint and the installed skill. `sync --check` exits with status `0`, which means the files on disk match the configuration. `ls` prints the path of the skill's `SKILL.md`.

## 9. Clean up

Stop the containers and delete this tutorial's data volume, return to the directory you started from, and remove the temporary directory:

```bash
docker compose down -v
cd -
rm -rf "$TUTORIAL_ROOT"
```

`down -v` deletes only the volume of the `marketplace-local-tutorial` project from step 1.

## What happened

The repository's `compose.yaml` started four services: the Marketplace API and worker (`app`), the root-mounted Nuxt Marketplace UI (`web`), the documentation-only VitePress service (`docs`), and an Nginx reverse proxy (`proxy`) that is the only service published on port `8080`. You configured a Marketplace on `localhost`, let it mirror a public registry, and installed one skill pinned to an exact revision.

## Next steps

- [How to deploy a production Marketplace](../how-to/operator/deploy-a-production-marketplace.md)
- [Marketplace operator configuration reference](../reference/marketplace-operator-configuration.md)
