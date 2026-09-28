# Adopting agent-marketplace

This guide covers setting up `agent-marketplace` in a repository and on a workstation, and the day-to-day configuration tasks that follow. For the detailed precedence rules (layer order, excludes, collection behavior, collisions, and error cases), see [`docs/PRECEDENCE.md`](./PRECEDENCE.md).

V4 (`apiVersion: agent-marketplace/v4`) is the canonical writable schema, built on `sources` and `resources`. Every new config root is v4. The v2 schema (`apiVersion: agent-marketplace/v2`) and its v3 compatibility alias are deprecated but stay fully readable and writable. Every mutation of a v2/v3 layer emits a `config.legacy-deprecated` warning naming the `agent-marketplace migrate` command for that scope.

A regular root without `apiVersion` (v1) is rejected. Interactive CLI startup and the TUI offer to migrate only the user-personal v2/v3 root (`~/.agents/config.yaml`); repository and personal-local roots are migrated only when you run `migrate` yourself. Declining, or running non-interactively, keeps the legacy reader in use. `agent-marketplace migrate` targets v4 by default; use `--to v2` only for legacy structural remediation.

## 30-second setup

A project config may declare its own target harness set. When it does not, the targets come from `~/.agents/config.yaml`, then `/etc/agent-marketplace/config.yaml`.

```bash
mkdir -p ~/.agents .agents

# Machine-wide target harness set
cat > ~/.agents/config.yaml << 'EOF_CONFIG'
apiVersion: agent-marketplace/v4
harnesses: [factory]
EOF_CONFIG

# Repo config binds a source and selects resources
cat > .agents/config.yaml << 'EOF_CONFIG'
apiVersion: agent-marketplace/v4
sources:
  infra:
    git: git@gitlab.example.com:example-user/infra-skills.git
    ref: main
    path: skills
    source_harness: claude
resources:
  infra:
    include: [skill/vm-lifecycle, skill/enc-debugging]
EOF_CONFIG

agent-marketplace sync
agent-marketplace status
git add .agents/config.yaml .agents/lock.yaml
git commit -m "Add shared infrastructure skills"
```

Sync writes Factory output at `.factory/skills/<name>/SKILL.md`. Every other harness listed in the effective `harnesses:` gets its own native layout.

Sync also writes `.agents/lock.yaml` in the v2 lock format. Anyone who reads that committed lock, including CI, needs agent-marketplace v4.0.9 or newer.

## Add Codex

Select Codex in the TUI harness modal (`H`) or add `codex` to the machine-wide target set. Existing installs and `migrate` do not add it for you:

```yaml
# ~/.agents/config.yaml
apiVersion: agent-marketplace/v4
harnesses: [factory, codex]
```

Codex uses split native roots:

| Scope | Skills and converted commands | Subagents, hooks, and MCP servers |
|---|---|---|
| Repository | `<repo>/.agents/skills/` | `<repo>/.codex/agents/`, `<repo>/.codex/hooks.json`, `<repo>/.codex/config.toml` |
| Machine | `~/.agents/skills/` | `~/.codex/`, or an absolute `$CODEX_HOME` |

`CODEX_HOME` does not move `~/.agents/skills`.

Keep generated `.agents/skills/` and `.codex/` out of source control. Codex rules, `AGENTS.md` generation, and legacy prompt output are out of scope. A registry can still use `source_harness: codex`, and `convert --from codex` and `--to codex` are supported. Conversion to Codex requires a destination without an existing `.codex/hooks.json` or `.codex/config.toml`; use a new or empty destination so shared configuration is never overwritten. Sync, check, ingestion, and conversion never install, require, or probe a Codex executable.

Repository `hooks.json` and `config.toml` changes may require Codex project trust review. Agent Marketplace reports when that happens, but it never edits trust state or adds Codex output paths to `.gitignore`. It preserves handwritten entries in these shared files and records its ownership outside the repository under `$XDG_STATE_HOME/agent-marketplace`. If that ownership state is missing or corrupt, sync refuses collisions and skips pruning instead of guessing.

## Registry allowlist

`allowed_registries` is optional:

- When it is omitted, all remote registries are permitted.
- An explicitly empty list denies all remote registries.
- A non-empty list of regex patterns permits only matching URLs. Patterns are matched against the URL in `host/path` form, without the scheme, `git@` prefix, or `.git` suffix.

The first authority layer that declares the allowlist sets it (organization, then user, then repository). `config.local.yaml` cannot declare it.

```yaml
# Permit only registries under the company GitLab instance
allowed_registries:
  - '^gitlab\.example\.com/'
```

Use single quotes for patterns that contain backslashes. In a double-quoted YAML string, `\.` is an invalid escape and the config fails to parse.

## Remediating structurally invalid config

A config with no `apiVersion` (v1) is rejected. `migrate --to v2` also refuses v1 input, so convert a v1 file to a v2 document by hand. For example:

Before (v1, rejected):

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

Use `migrate --to v2` for structural issues in v2/v3 files, such as legacy collection syntax:

```bash
agent-marketplace migrate --to v2 --harnesses factory,cursor
agent-marketplace sync
```

`migrate --to v2` works only when every existing writable root is v2 or v3; it refuses a v4 root. It does not rewrite valid v2 or v3 files, and a project-level `harnesses:` stays in the project config. Remediated output uses canonical v2.

## Explicitly migrate v2/v3 to v4

V4 migration changes the format only and runs only with your consent. Interactive CLI startup and the TUI offer it for the user-personal root. To review a conversion first, run an explicit dry run:

```bash
agent-marketplace migrate --dry-run
```

By default the command validates the writable user, repository, and repository-personal files together. To limit it to one layer, use `--scope user`, `--scope repo`, or `--scope local`:

```bash
agent-marketplace migrate --scope repo
```

What the conversion does:

- It never touches organization configuration.
- Git/directory-only conversion leaves generated locks unchanged.
- For a config with exact legacy Marketplace pins backed by verified local cache evidence, it also writes `sources.<name>.marketplace` endpoints, direct requests under `resources`, and a generated `agent-marketplace-lock/v2` record with the identity, integrity, and selection state. The config and lock are published together in one journaled transaction.
- It preserves semantics and selection behavior for representable inputs. It regenerates canonical v4 YAML and does not keep authored comments or formatting.
- It validates the generated v4 documents with the normal runtime adapter before publication.
- Every migrated config gets an adjacent owner-readable backup named `<config>.pre-v4.<UTC timestamp>.bak` containing the exact legacy bytes. Backups, replacements, and any lock changes publish atomically, and earlier publications roll back if a later one fails.
- `--dry-run` performs these checks without writing a backup, config, or lock.

The conversion refuses and changes nothing when it meets:

- a legacy `include` tree
- hosted Marketplace selections without proven pin provenance (missing verified cache evidence leaves the originals readable and unchanged)
- collection-derived aliases that cannot be represented
- top-level kind selectors in a personal-local config, which have no source to attach to in v4
- a repository-relative local registry in the user config
- any other selector or policy that cannot be represented

After conversion, normal CLI readers use explicit Git/directory v4 roots for discovery, status, and synchronization, and hydrate explicit v4 Marketplace requests from the matching v2 lock. Use `directory` for live local content, even when its name ends in `.git`. A directory source resolves from the declaring file, does not use a Git lock, and does not accept the Git-only `ref` or `path`.

The v4 runtime keeps decisions in order. A v4 `required: true` group on its own selects zero resources, so legacy implicit all-selection is written explicitly as `all: true`. Explicit empty `tags: []` and `harnesses: []` overrides stay distinct from omitted fields.

Install, remove, update, and TUI edits then work directly on v4 roots. They edit Git/directory sources, resource groups, exact Marketplace sources, requests, and lock evidence, and publish each config and lock change atomically. Raw legacy writer APIs still return `v4.writer-opt-in` instead of rewriting v4 as legacy YAML.

Some fields are not valid in every v4 layer and are rejected with `v4.layer-field`:

| Layer | Rejected fields |
|---|---|
| Repository-personal (`config.local.yaml`) | `harnesses`, `allowed_registries`, `allowed_marketplaces`, `backup`, `logging`, `telemetry` |
| Repository (`config.yaml`) | `backup`, `logging`, `telemetry` |
| Organization | `materialization` |

If migration is declined, runs non-interactively, or refuses an unsupported include or Marketplace input, the original v2/v3 configuration stays readable and unchanged, and each later mutation warns once per transaction with `config.legacy-deprecated`.

Keep `.agents/skills/` ignored when `codex` is selected; otherwise remove legacy ignore entries you no longer need. Do not commit generated `.factory`, `.cursor`, `.claude`, `.gemini`, `.windsurf`, `.devin`, or `.codex` output unless your repository tracks harness configuration on purpose.

## Install by tag

```bash
agent-marketplace install --tag puppet
```

`--tag` writes to the repository config only (`--scope repo`, the default). Without `--kind`, it adds the tag to the resource group of each configured source that has a catalog. In a v4 root that looks like this:

```yaml
resources:
  infra:
    include: [skill/vm-lifecycle, skill/enc-debugging]
    tags: [puppet]
```

To exclude specific skills, add exclusions to the committed repo config or to your personal local config. In v4, an exclusion is a `kind/name` locator in the source's resource group:

```yaml
resources:
  infra:
    exclude: [skill/firewall-rules-management]
```

`exclude: [skill/*]` excludes every skill from the source while keeping other kinds, and `exclude: ["*"]` excludes everything from it.

In a v2/v3 root, the same exclusions use per-kind selectors:

```yaml
skills:
  exclude: [firewall-rules-management]
```

Exclude all skills (or another kind) from one registry entry with `exclude_all`:

```yaml
# Per-kind: exclude all skills, keep commands/subagents
registries:
  - url: ./plugins/testing
    skills:
      exclude_all: true
```

```yaml
# Whole plugin: exclude everything
registries:
  - url: ./plugins/testing
    exclude_all: true
```

At a higher-precedence layer (for example `config.local.yaml`), `exclude_all` on a registry entry is scoped by URL and path subtree. It suppresses resources from the same registry at lower layers only when the resource is at the same path as the excluding entry or below it, by whole path components. A root entry covers its subpaths, and sibling paths stay independent. A concrete ref further limits the shadow to that ref; an omitted ref stays a wildcard for legacy exclusions. In a legacy `config.local.yaml`, top-level `skills.exclude_all: true` excludes all repo-scoped skills regardless of registry.

## Install by collection

```bash
agent-marketplace list --kind collection
agent-marketplace install platform-onboarding
```

In a v4 root this adds a `collection/` locator to the resource group of the source that provides the collection:

```yaml
resources:
  infra:
    include: [skill/vm-lifecycle, skill/enc-debugging, collection/platform-onboarding]
```

In a v2/v3 root it adds the name to the providing registry entry's `collections.include` list.

Removing a collection member keeps the bundle and writes an exclusion. Removing the collection name deletes the selection from the selected root, or writes a collection exclusion there when an included file owns the selection.

## Personal overrides

Create `.agents/config.local.yaml` for choices that apply only to your checkout. A v4 local root declares the source it refers to, then includes or excludes resources from it:

```yaml
apiVersion: agent-marketplace/v4
sources:
  infra-skills:
    git: git@gitlab.example.com:example-user/infra-skills.git
    ref: main
    path: skills
    source_harness: claude
resources:
  infra-skills:
    include: [skill/my-personal-skill]
    exclude: [skill/some-skill-you-dont-want]
```

`agent-marketplace remove <name> --scope local` writes this kind of file for you. It creates a v4 local root when none exists and redeclares the providing source there.

A v2/v3 local root can instead use top-level selectors that apply across registries:

```yaml
skills:
  include: [my-personal-skill]
  exclude: [some-skill-you-dont-want]
```

V4 has no source-free equivalent, so `migrate` refuses a local config that uses these top-level selectors.

When the personal-local layer exists, `agent-marketplace sync` adds `.agents/config.local.yaml` and `.agents/lock.local.yaml` to `.gitignore`. Local excludes apply only to non-required repo-scoped resources; you cannot locally exclude machine-scoped or required resources.

## Git dependency locks

Remote Git requirements resolve to generated lockfiles owned by their configuration layers: `/etc/agent-marketplace/lock.yaml`, `~/.agents/lock.yaml`, `.agents/lock.yaml`, and `.agents/lock.local.yaml`. Commit the repository lock with `.agents/config.yaml`, and keep the personal-local lock private.

A deployed organization lock is validated with the organization config and used exactly. When it is absent, consumer commands may inspect or refresh that unlocked layer through the mutable cache, but they do not create the organization lock. An invalid lock that is present never falls back to this path.

How the commands treat locks:

- Normal `sync` keeps compatible exact commits and makes the smallest lock changes needed after a configuration change.
- `sync --locked` fails instead of changing config or locks.
- `sync --check` and `lock --check` are read-only. They do not recover transactions, resolve mutable refs, fetch or populate a cold cache, or create state roots. If a required exact snapshot is not available locally, check mode reports it as unavailable.
- `lock --upgrade` and `update` advance Git resolutions when you ask for it.

Lock reconciliation protects exact snapshots with a shared cache lease from acquisition through config-and-lock publication. Install, remove, update, and TUI config changes publish config and lock state transactionally. Configured install discovery uses the exact inventory for each valid locked layer, and falls back to a mutable ref only for a registry whose own layer lock is missing.

Plain local-directory registries are read live and never locked. Lock v2 is the only write format, including for Git-only layers. A writable v1 lock upgrades on the next reconciliation. Lock v2 also carries the exact Marketplace endpoint, registry, resource, and selection records that v4 layers use.

### Removing a resource enabled in two scopes

When a non-required resource is enabled in both machine and repo scopes, `remove` without `--scope` reports the ambiguity.

- `remove --scope user` or `remove --scope repo` removes only the selected occurrence; the other scope stays enabled.
- An explicit scope never falls back to another scope when that scope has no occurrence.
- A machine-owned occurrence targeted from repo or local scope produces a boundary error.
- `--scope local` is for checkout-local selections and excludes, and cannot bypass the machine/repo boundary.
- An unscoped removal with exactly one occurrence infers its effective layer. The exception is a repo-scoped auto-selected resource that the committed repo config does not explicitly select: it is recorded as a checkout-local exclusion. Use `--scope repo` to edit committed repo config.

## Install from a Marketplace

First install the CLI and configure at least one target harness as described in [30-second setup](#30-second-setup). A Marketplace resource page shows an install command with a full URL pinned to one exact revision:

```bash
agent-marketplace install "https://marketplace.example/r/example/skill/review/revisions/sha256%3A..."
```

Run that command as shown. The Marketplace does not need to be registered first. To make the Marketplace available for later CLI discovery and shorter locators, you can register its base URL:

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
agent-marketplace migrate --dry-run
agent-marketplace convert --from claude --to factory --out ./factory-plugin ./claude-plugin
agent-marketplace doctor
agent-marketplace doctor --repair
```

## Ad-hoc registry operations (`--from`)

Use `--from <url>` to work with a registry URL directly, without configuring it first. `list`, `install`, and `remove` all support `--from`, with optional `--ref` and `--path` flags.

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

Flag rules:

- `--ref` and `--path` require `--from`.
- `install --from` cannot be combined with `--tag` or `--all`.
- `list --from` only browses.
- `remove --from` without a name deletes the entry from the selected root (repo by default). If the entry comes from an included file or a lower layer, the command writes an `exclude_all` shadow in the selected root instead, and leaves the source file unchanged.
- For a named removal, the URL, path, and ref identify the configured registry entry, and `--path` narrows matching to a configured subdirectory and its discovered descendants.
- Omitting `--path` or `--ref` is unambiguous only when the URL identifies one logical configured location. Distinct paths or refs require the corresponding flag.
- Use the entry's authored path spelling when possible; equivalent normalized URL/path spellings also identify it.
- Diagnostic `@ref` labels are for display only. Pass the URL, path, and ref as separate flags.
- Normal scope boundaries still apply.

## How sync works

1. Load org, user, repo, and local config, recording which layer each value came from.
2. Validate existing locks, keep compatible Git resolutions, and update stale writable locks unless locked or check mode forbids writes.
3. Open each Git source at its exact commit and verify its source manifest; read plain directory registries live.
4. Read registry content through `harness-compilation`.
5. Resolve the five kinds that are written into harness folders (skills, commands, subagents, hooks, and MCP servers), plus collections, tags, and excludes.
6. Compile each resolved resource for each configured harness.
7. Write the native harness files and prune stale generated output.

Run `agent-marketplace status` to see the selected harnesses, native targets, cache freshness, local excludes, orphaned files, collisions, and errors.
