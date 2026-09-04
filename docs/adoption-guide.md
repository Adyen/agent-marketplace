# Adopting agent-marketplace

For detailed precedence rules (layer order, excludes, collection behavior, collision/error cases), see [`docs/PRECEDENCE.md`](./PRECEDENCE.md).

The **v2 schema** (`apiVersion: agent-marketplace/v2`) is canonical. The loader accepts v3 as a compatibility alias and rejects v1 configs (no `apiVersion`). `agent-marketplace migrate` performs structural remediation without rewriting valid v2 or v3 files.

## 30-second setup

Project configs may declare their target harness set. When they do not, targets fall back to `~/.agents/config.yaml` and `/etc/agent-marketplace/config.yaml`.

```bash
mkdir -p ~/.agents .agents

# Machine-wide target harness set
cat > ~/.agents/config.yaml << 'EOF_CONFIG'
apiVersion: agent-marketplace/v2
harnesses: [factory]
registries: []
EOF_CONFIG

# Repo config selects resources from registries
cat > .agents/config.yaml << 'EOF_CONFIG'
apiVersion: agent-marketplace/v2
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

Factory output is generated at `.factory/skills/<name>/SKILL.md`. Other harnesses get their own native layouts when listed in the effective `harnesses:`.

## Add Codex

Select Codex in the TUI harness modal (`H`) or add `codex` to the machine-wide
target set. Existing installs and `migrate` do not add it automatically:

```yaml
# ~/.agents/config.yaml
apiVersion: agent-marketplace/v3
harnesses: [factory, codex]
```

Codex uses split native roots. Repo-scoped skills and converted commands go to
`<repo>/.agents/skills/`; the other three target-materialized kinds go to
`<repo>/.codex/agents/`, `<repo>/.codex/hooks.json`, and
`<repo>/.codex/config.toml`. Machine-scoped skills and commands go to
`~/.agents/skills/`; subagents, hooks, and MCP servers go to `~/.codex/` or an
absolute `$CODEX_HOME`. `CODEX_HOME` does not move `~/.agents/skills`.

Keep `.agents/skills/` and `.codex/` out of source control when they are
generated. Codex rules, `AGENTS.md` generation, and legacy prompt output are
explicitly excluded. A registry can still use `source_harness: codex`, and
`convert --from codex` / `--to codex` are supported. Conversion to Codex
requires a destination without pre-existing `.codex/hooks.json` or
`.codex/config.toml`; use a new or empty destination so shared configuration is
never overwritten. Sync, check, ingestion, and conversion never install,
require, or probe a Codex executable.

Repository `hooks.json` and `config.toml` changes may require Codex project
trust review. Agent Marketplace reports that boundary but never edits trust
state or adds Codex output paths to `.gitignore`. It preserves handwritten
aggregate entries and records ownership outside the repo under
`$XDG_STATE_HOME/agent-marketplace`; missing or corrupt ownership state refuses
collisions and pruning rather than guessing.

## Registry allowlist

`allowed_registries` is optional. When it is omitted, all remote registries are permitted. An explicitly empty list denies all remote registries, and a non-empty list of regex patterns permits only matching URLs. The allowlist is set by the first authority layer that declares it (org > user > repo); `config.local.yaml` cannot declare it.

```yaml
# Permit only registries under the company GitLab instance
allowed_registries:
  - "^gitlab\.example\.com/"
```

## Remediating a legacy config

v1 configs (no `apiVersion`) are rejected by the loader. Use `migrate` for structural issues such as legacy collection syntax; it preserves the canonical v2 apiVersion:

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

After (v2 repo config):

```yaml
apiVersion: agent-marketplace/v2
registries:
  - url: git@gitlab.example.com:example-user/infra-skills.git
    ref: main
    skills:
      include: [vm-lifecycle]
```

`migrate` does not rewrite valid v2 or v3 files, and project-level `harnesses:` remains in the project config.

Keep `.agents/skills/` ignored when `codex` is selected; otherwise remove legacy
ignore entries when they are no longer needed. Do not commit generated
`.factory`, `.cursor`, `.claude`, `.gemini`, `.windsurf`, `.devin`, or `.codex`
output unless your repo intentionally tracks harness configuration.

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

At a higher-precedence layer (e.g. `config.local.yaml`), `exclude_all` on a registry entry is scoped by URL and path subtree -- it suppresses resources from the same registry at lower layers only when the resource is at the same path or a component-wise descendant of the excluding entry (a root entry covers subpaths; sibling paths remain independent). A concrete ref further limits the shadow to that ref; an omitted ref remains a wildcard for legacy exclusions. Top-level `skills.exclude_all: true` in `config.local.yaml` excludes all repo-scoped skills regardless of registry.

## Install by collection

```bash
agent-marketplace list --kind collection
agent-marketplace install platform-onboarding
```

This writes:

```yaml
collections:
  include:
    - platform-onboarding
```

Removing a collection member keeps the bundle and writes an `exclude`. Removing the collection name deletes the selection from the selected root, or writes a collection exclusion in the root when an included file owns it.

## Personal overrides

Create `.agents/config.local.yaml` for checkout-only choices:

```yaml
skills:
  include: [my-personal-skill]
  exclude: [some-skill-you-dont-want]
```

`agent-marketplace sync` adds `.agents/config.local.yaml` to `.gitignore` automatically when the file exists. Local excludes apply only to non-required repo-scoped resources; machine-scoped resources and required resources cannot be locally excluded.

When a non-required resource is enabled in both machine and repo scopes,
`remove` without `--scope` reports the ambiguity. Use `remove --scope user` or
`remove --scope repo` to remove only the selected occurrence; the other scope
remains enabled. An explicit scope never falls back to another scope when that
scope has no occurrence; it reports that the resource is not enabled (and a
machine-owned occurrence targeted from repo/local scope produces a boundary
error). `--scope local` is for checkout-local selections/excludes, and cannot
bypass the machine/repo boundary. An unscoped removal with one unambiguous
occurrence infers its effective layer (machine resources use user config),
except that a repo-scoped auto-selected resource that is not explicitly selected
by committed repo config is recorded as a checkout-local exclusion. Use
`--scope repo` to edit committed repo config.

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
agent-marketplace doctor --repair
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

# Remove the entire ad-hoc registry at this scope
agent-marketplace remove --from git@gitlab.example.com:example-org/infra-skills
```

Flag rules: `--ref` and `--path` require `--from`; `install --from` is incompatible with `--tag`/`--all`; `list --from` is browse-only. `remove --from` without a name deletes an entry from the selected root (repo by default). If the entry comes from an included file or a lower layer, the command writes an `exclude_all` shadow in the selected root instead. For named removal, the URL, path, and ref identify the configured registry entry; `--path` narrows matching to a configured subdirectory and its discovered descendants. Omitting `--path` or `--ref` is only unambiguous when the URL identifies one logical configured location; distinct paths or refs require the corresponding flag. Use the entry's authored path spelling when possible; equivalent normalized URL/path spellings also identify it. Diagnostic `@ref` labels are display-only: pass URL, path, and ref as separate flags. The source file remains unchanged, and normal scope boundaries still apply.

## How sync works

1. Load org, user, repo, and local config.
2. Fetch registry caches under `~/.cache/agent-marketplace/`.
3. Ingest registry content through `harness-compilation`.
4. Resolve the five target-materialized kinds (skills, commands, subagents,
   hooks, and MCP servers), plus collections, tags, and excludes.
5. Compile each resolved resource to each configured harness.
6. Materialize native harness files with prune-stale enabled.

Run `agent-marketplace status` to see the selected harnesses, native targets, cache freshness, local excludes, orphaned files, collisions, and errors.
