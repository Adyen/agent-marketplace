# Adopting agent-marketplace

For detailed precedence rules (layer order, excludes, collection behavior, collision/error cases), see [`docs/PRECEDENCE.md`](./PRECEDENCE.md).

Legacy configuration uses the **v2 schema** (`apiVersion: agent-marketplace/v2`); the loader accepts v3 as a compatibility alias and rejects v1 configs with no `apiVersion`. V4 is a separate `sources`/`resources` schema (`apiVersion: agent-marketplace/v4`) that normal CLI readers can consume. `agent-marketplace migrate` does not convert v2/v3 implicitly: omit `--to` for legacy structural remediation, or use the explicit `--to v4` conversion described below.

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
git add .agents/config.yaml .agents/lock.yaml
git commit -m "Add shared infrastructure skills"
```

Factory output is generated at `.factory/skills/<name>/SKILL.md`. Other harnesses get their own native layouts when listed in the effective `harnesses:`.

## Add Codex

Select Codex in the TUI harness modal (`H`) or add `codex` to the machine-wide
target set. Existing installs and `migrate` do not add it automatically:

```yaml
# ~/.agents/config.yaml
apiVersion: agent-marketplace/v2
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

## Remediating structurally invalid config

v1 configs with no `apiVersion` are rejected and must be converted to a supported legacy v2 document before they can be loaded. Use `migrate` without `--to` for structural issues in v2/v3 files, such as legacy collection syntax:

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

`migrate` without `--to` does not rewrite valid v2 or v3 files, and project-level `harnesses:` remains in the project config. Structurally remediated output uses canonical v2.

## Explicitly migrate v2/v3 to v4

V4 migration is opt-in and format-only. Preview it before publishing:

```bash
agent-marketplace migrate --to v4 --dry-run
```

By default this validates the writable user, repository, and repository-personal files together. To limit the operation to one layer, use `--scope user`, `--scope repo`, or `--scope local`:

```bash
agent-marketplace migrate --to v4 --scope repo
```

Organization configuration is never touched, and the migration never changes lockfiles. A successful conversion writes `apiVersion: agent-marketplace/v4` with named `sources` and `resources`; normal CLI readers then consume Git/directory v4 roots for discovery, status, and synchronization. A Marketplace source is parsed, but the legacy runtime adapter requires an acquisition lock and reports `v4.marketplace-lock-required` when one is not available. V4 roots and fragments must use the v4 document shape; legacy keys such as `registries`, `skills`, and `marketplaces` are not mixed into them.

The conversion is fail-closed and lossless. It refuses legacy `include` trees rather than flattening or editing their fragments, and refuses hosted Marketplace selections when their saved pin does not prove the provenance needed for a v4 request. Unsupported source or selector forms are also refused. Every selected input is validated before publication, so a failure leaves the legacy files unchanged. `--dry-run` performs these checks without writing.

After conversion, legacy configuration writers protect the v4 root: install/remove and other legacy edits return a `v4.writer-opt-in` diagnostic rather than rewriting v4 as legacy YAML. Edit the v4 source/resource declarations directly for subsequent changes.

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

`agent-marketplace sync` adds `.agents/config.local.yaml` and `.agents/lock.local.yaml` to `.gitignore` automatically when the personal-local layer exists. Local excludes apply only to non-required repo-scoped resources; machine-scoped resources and required resources cannot be locally excluded.

## Git dependency locks

Remote Git requirements resolve to generated lockfiles owned by their configuration layers: `/etc/agent-marketplace/lock.yaml`, `~/.agents/lock.yaml`, `.agents/lock.yaml`, and `.agents/lock.local.yaml`. Commit the repository lock with `.agents/config.yaml`; keep the personal-local lock private. A deployed organization lock is validated with the organization config and used exactly. When it is absent, consumer commands may inspect or refresh that intentionally unlocked layer through the mutable cache but do not create the organization lock; an invalid present lock never falls back to this path.

Normal sync retains compatible exact commits and minimally reconciles writable locks after configuration changes. `sync --locked` fails instead of changing config or locks. `sync --check` and `lock --check` are read-only: they do not recover transactions, resolve mutable refs, fetch or populate a cold cache, or create state roots. If a required exact snapshot is absent locally, check mode reports it as unavailable. Use `lock --upgrade` or `update` to advance Git resolutions deliberately. Lock reconciliation protects exact snapshots with a shared cache lease from acquisition through config-and-lock publication. Install, remove, update, and TUI config changes publish config and lock state transactionally; configured install discovery uses exact inventory for each valid locked layer and falls back to a mutable ref only for a registry whose own layer lock is missing.

Plain local-directory registries are read live and never locked. This foundation does not include hosted Marketplace lock records, acquisition adapters, or integration.

When a non-required resource is enabled in both machine and repo scopes, `remove` without `--scope` reports the ambiguity. Use `remove --scope user` or `remove --scope repo` to remove only the selected occurrence; the other scope remains enabled. An explicit scope never falls back to another scope when that scope has no occurrence. A machine-owned occurrence targeted from repo or local scope produces a boundary error. `--scope local` is for checkout-local selections and excludes and cannot bypass the machine/repo boundary. An unscoped removal with one unambiguous occurrence infers its effective layer, except that a repo-scoped auto-selected resource not explicitly selected by committed repo config is recorded as a checkout-local exclusion. Use `--scope repo` to edit committed repo config.

## Install from a Marketplace

First install the CLI and configure at least one target harness as described in
[30-second setup](#30-second-setup). A Marketplace resource page provides an
install command with a full URL pinned to one exact revision:

```bash
agent-marketplace install "https://marketplace.example/r/example/skill/review/revisions/sha256%3A..."
```

Run that command directly. The Marketplace does not need to be registered
first. To make the Marketplace available for later CLI discovery and shorter
locators, optionally register its base URL:

```bash
agent-marketplace install "https://marketplace.example"
```

Registration adds a Marketplace source. It does not install a resource.

## Daily commands

```bash
agent-marketplace list
agent-marketplace install <skill>
agent-marketplace remove <skill>
agent-marketplace lock
agent-marketplace lock --upgrade
agent-marketplace sync
agent-marketplace sync --locked
agent-marketplace sync --check
agent-marketplace status
agent-marketplace update
agent-marketplace migrate --to v4 --dry-run
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

1. Load org, user, repo, and local config with their layer provenance.
2. Validate existing locks, retain compatible Git resolutions, and reconcile stale writable locks unless locked/check mode forbids writes.
3. Open each Git source by exact commit and verify its source manifest; read plain directory registries live.
4. Ingest registry content through `harness-compilation`.
5. Resolve the five target-materialized kinds (skills, commands, subagents,
   hooks, and MCP servers), plus collections, tags, and excludes.
6. Compile each resolved resource to each configured harness.
7. Materialize native harness files and prune stale generated output.

Run `agent-marketplace status` to see the selected harnesses, native targets, cache freshness, local excludes, orphaned files, collisions, and errors.
