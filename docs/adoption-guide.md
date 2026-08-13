# Adopting agent-marketplace

For detailed precedence rules (layer order, excludes, collection behavior, collision/error cases), see [`docs/PRECEDENCE.md`](./PRECEDENCE.md).

All configs use the **v3 schema** (`apiVersion: agent-marketplace/v3`). The loader rejects v1 configs (no `apiVersion`). v2 configs can be rewritten to v3 with `agent-marketplace migrate`.

## 30-second setup

The target harness set is machine-wide: it lives in `~/.agents/config.yaml` (or `/etc/agent-marketplace/config.yaml`). Repo and local configs only select resources.

```bash
mkdir -p ~/.agents .agents

# Machine-wide target harness set
cat > ~/.agents/config.yaml << 'EOF_CONFIG'
apiVersion: agent-marketplace/v3
harnesses: [factory]
registries: []
EOF_CONFIG

# Repo config selects resources from registries
cat > .agents/config.yaml << 'EOF_CONFIG'
apiVersion: agent-marketplace/v3
registries:
  - url: git@gitlab.example.com:example-user/infra-skills.git
    ref: main
    source_harness: claude
    path: skills
    skills:
      include: [vm-lifecycle, enc-debugging]
EOF_CONFIG

agent-marketplace sync
agent-marketplace status
git add .agents/config.yaml
git commit -m "Add shared infrastructure skills"
```

Factory output is generated at `.factory/skills/<name>/SKILL.md`. Other harnesses get their own native layouts when listed in your machine-wide `harnesses:`.

## Registry allowlist

`allowed_registries` is optional. When it is omitted, all remote registries are permitted. An explicitly empty list denies all remote registries, and a non-empty list of regex patterns permits only matching URLs. The allowlist is set by the first authority layer that declares it (org > user > repo); `config.local.yaml` cannot declare it.

```yaml
# Permit only registries under the company GitLab instance
allowed_registries:
  - "^gitlab\.example\.com/"
```

## Migrating a legacy v1 repo

v1 configs (no `apiVersion`) are rejected by the loader. If you have an old v2 config, `migrate` rewrites it to v3:

```bash
agent-marketplace migrate --harnesses factory,cursor
agent-marketplace sync
```

Before (v1, no longer accepted):

```yaml
registries:
  - url: git@gitlab.example.com:example-user/infra-skills.git
    ref: main
skills:
  - vm-lifecycle
```

After (v3 repo config):

```yaml
apiVersion: agent-marketplace/v3
registries:
  - url: git@gitlab.example.com:example-user/infra-skills.git
    ref: main
    skills:
      include: [vm-lifecycle]
```

`migrate` writes the machine-wide `harnesses:` list to `~/.agents/config.yaml` and removes top-level `harnesses` from repo and local configs.

Remove old `.agents/skills/` gitignore entries when they are no longer needed. Do not commit generated `.factory`, `.cursor`, `.claude`, `.gemini`, or `.windsurf` outputs unless your repo intentionally tracks harness config.

## Install by tag

```bash
agent-marketplace install --tag puppet
```

Exclude specific skills with committed repo config or with personal local config:

```yaml
skills:
  exclude: [firewall-rules-management]
```

Exclude all skills (or other kinds) from a registry entry using `exclude_all`:

```yaml
# Per-kind: exclude all skills, keep commands/subagents
registries:
  - url: ./plugins/testing
    skills:
      exclude_all: true

# Whole plugin: exclude everything
registries:
  - url: ./plugins/testing
    exclude_all: true
```

At a higher-precedence layer (e.g. `config.local.yaml`), `exclude_all` on a registry entry is scoped by URL -- it only suppresses resources from the same registry at lower layers. Top-level `skills.exclude_all: true` in `config.local.yaml` excludes all repo-scoped skills regardless of registry.

## Install by collection

```bash
agent-marketplace list --collections
agent-marketplace list --collection platform-onboarding
agent-marketplace install platform-onboarding
```

This writes:

```yaml
collections:
  include:
    - platform-onboarding
```

Removing a collection member keeps the bundle and writes an `exclude`; removing the collection name removes the whole bundle reference.

## Personal overrides

Create `.agents/config.local.yaml` for checkout-only choices:

```yaml
skills:
  include: [my-personal-skill]
  exclude: [some-skill-you-dont-want]
```

`agent-marketplace sync` adds `.agents/config.local.yaml` to `.gitignore` automatically when the file exists. Required skills cannot be locally excluded.

## Daily commands

```bash
agent-marketplace list
agent-marketplace install <skill>
agent-marketplace remove <skill>
agent-marketplace sync
agent-marketplace sync --check
agent-marketplace status
agent-marketplace update
agent-marketplace convert --from claude --to factory --out ./factory-plugin ./claude-plugin
agent-marketplace doctor
```

## Ad-hoc registry operations (`--from`)

Use `--from <url>` to work with a registry URL directly, without pre-configuring it. All three commands (`list`, `install`, `remove`) support `--from` with optional `--ref` and `--path` flags.

```bash
# Browse resources at a URL — no config is written
agent-marketplace list --from git@gitlab.example.com:example-org/infra-skills --ref main

# Install a single resource from an ad-hoc registry
agent-marketplace install vm-lifecycle --from git@gitlab.example.com:example-org/infra-skills --ref main

# Install all resources (bundle mode)
agent-marketplace install --from git@gitlab.example.com:example-org/infra-skills --ref main

# Install only one kind
agent-marketplace install --from git@gitlab.example.com:example-org/infra-skills --ref main --kind command

# Remove a specific resource from that registry
agent-marketplace remove vm-lifecycle --from git@gitlab.example.com:example-org/infra-skills

# Remove the entire ad-hoc registry entry (all resources + the entry)
agent-marketplace remove --from git@gitlab.example.com:example-org/infra-skills
```

Flag rules: `--ref` and `--path` require `--from`; `install --from` is incompatible with `--tag`/`--all`; `remove --from` without a name removes the entire entry; `list --from` is browse-only.

## How sync works

1. Load org, user, repo, and local config.
2. Fetch registry caches under `~/.cache/agent-marketplace/`.
3. Ingest registry content through `harness-compilation`.
4. Resolve skills, collections, tags, and excludes.
5. Compile each resolved resource to each configured harness.
6. Materialize native harness files with prune-stale enabled.

Run `agent-marketplace status` to see the selected harnesses, native targets, cache freshness, local excludes, orphaned files, collisions, and errors.
