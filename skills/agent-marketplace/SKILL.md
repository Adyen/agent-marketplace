---
name: agent-marketplace
description: >
  How to use the agent-marketplace CLI to install shared AI agent resources
  from Git, directory, or hosted Marketplace sources and materialize them into
  native harness layouts. Use when setting up a repo, writing
  `.agents/config.yaml`, installing/removing skills, commands, subagents,
  hooks, MCP servers, or collections, checking sync drift, updating locks,
  migrating legacy configs, or repairing shared harness config ownership.
metadata:
  tags:
    - cli
    - skills
    - setup
---

# agent-marketplace

The full user documentation lives in the repository's
[`site/docs`](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/index.md).
This skill caches what an agent needs most often; the
[CLI reference](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/reference/cli.md),
[configuration reference](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/reference/configuration.md),
and `agent-marketplace <command> --help` own the exhaustive detail.

## Quick Start

```bash
agent-marketplace init --harnesses claude,codex   # ~/.agents/config.yaml (v4, targets only)

mkdir -p .agents
cat > .agents/config.yaml << 'EOF'
apiVersion: agent-marketplace/v4
sources:
  infra:
    git: git@github.com:example-org/infra-skills.git
    ref: main
    path: skills
resources:
  infra:
    include:
      - skill/vm-lifecycle
      - command/deploy
EOF

agent-marketplace sync      # writes .agents/lock.yaml and native harness files
agent-marketplace status
git add .agents/config.yaml .agents/lock.yaml
```

Bare `agent-marketplace` on a terminal opens the TUI; on first launch it asks
for the target harnesses. When no user config exists, a plain `sync` creates
one with `harnesses: [factory, windsurf]`, so declare targets first.

## Configuration

`apiVersion: agent-marketplace/v4` is the canonical schema; `init`, `install`,
`remove`, and the TUI create v4 files. v2/v3 files (`registries:`, top-level
`skills:`) are still read but deprecated: every write to one warns with
`config.legacy-deprecated`. A root without `apiVersion` is rejected.

### Layers

| Layer | Config | Lock |
|---|---|---|
| Organization | `/etc/agent-marketplace/config.yaml` | `/etc/agent-marketplace/lock.yaml` |
| User | `~/.agents/config.yaml` | `~/.agents/lock.yaml` |
| Repository | `<repo>/.agents/config.yaml` | `<repo>/.agents/lock.yaml` |
| Repository personal | `<repo>/.agents/config.local.yaml` | `<repo>/.agents/lock.local.yaml` |

Layers merge organization → user → repository → personal. Targets come from the
first non-empty `harnesses:` list in repository, then user, then organization;
lists are never combined. Repo and personal selections install under repo
roots; user and organization selections install under machine roots. The
personal file cannot set `harnesses` or allowlists, and the CLI adds it and its
lock to `.gitignore`. Every layer supports local-file `include:` (one path or a
list); fragments merge below the including file, and editors refuse to edit a
selection that lives in a fragment (`config.edit.include-owned`).

### Sources and resource groups

```yaml
apiVersion: agent-marketplace/v4
harnesses: [claude, cursor, devin, codex]
sources:
  shared:                        # name is a label local to this file
    git: https://github.com/example-org/agent-resources.git
    ref: v1.4.0                  # required for git
    path: resources              # optional subdirectory
    source_harness: claude       # optional; omit to autodetect
  local:
    directory: ./agent-resources # read live, never locked
  company:
    marketplace: https://marketplace.example.com
resources:
  shared:
    include: [skill/code-review, command/release, collection/platform-onboarding]
    tags: [platform]
    exclude: [skill/legacy-review]
    harnesses: [claude]          # narrows targets, never widens
  local: [skill/scratch]         # plain list = include only
  company:
    - shared-agents/skill/review:
        revision: sha256:<64 lowercase hex>
```

- A source has exactly one of `git`, `directory`, or `marketplace`. Every
  `resources` key must name a source declared in the same file or its includes.
- Git/directory locators are `<kind>/<name>`; Marketplace locators are
  `<registry-slug>/<kind>/<name>`. Kinds: `skill`, `command`, `subagent`,
  `hook`, `mcp`, `collection`.
- Group fields: `include`, `exclude` (`*`, `<kind>/*`, `<kind>/<name>`),
  `tags`, `all: true`, `required: true` (protects selections from excludes and
  `remove`, selects nothing itself), `harnesses`.
- Exclusions apply after inclusions within a file. Required resources are never
  excluded.
- Relative `directory:` paths in `.agents/config.yaml`/`config.local.yaml`
  resolve from the repo root; in other files, from the declaring file.
- Git and directory resources take their version from the source `ref`; declare
  a second source for another ref. `revision` is Marketplace-only.
- Other root fields: `allowed_registries`, `allowed_marketplaces` (regex
  allowlists), `materialization` (`mode: symlink|file`, per-target `targets`),
  plus machine-only `backup`, `logging`, `telemetry`.

Unknown fields, YAML aliases/merge keys, and duplicate keys are rejected with
`v4.*` diagnostics; the message names the setting.

## Output paths

| Harness | Repo root | Machine root | Skill / command |
|---|---|---|---|
| `claude` | `.claude` | `~/.claude` | `skills/<name>/SKILL.md`, `commands/<name>.md` |
| `factory` | `.factory` | `~/.factory` | `skills/<name>/SKILL.md`, `commands/<name>.md` (always copied) |
| `cursor` | `.cursor` | `~/.cursor` | `skills/<name>/SKILL.md`, `commands/<name>.md` |
| `windsurf` | `<repo>` (`.windsurf/…`) | `~/.codeium/windsurf` | `skills/<name>/SKILL.md`, `workflows/` (machine: `global_workflows/`) |
| `gemini` | `.gemini` | `~/.gemini` | `skills/<name>/SKILL.md`, `commands/<name>.toml` |
| `devin` | `.devin` | `~/.config/devin` | `skills/<name>/SKILL.md` for both |
| `codex` | `.agents` + `.codex` | `~/.agents` + `$CODEX_HOME` or `~/.codex` | `.agents/skills/<name>/SKILL.md` for both |

Devin and Codex commands are skill directories whose `SKILL.md` carries
`x-agent-marketplace-kind: command`. Shared aggregate files hold hooks and MCP
servers: Claude MCP goes to `<repo>/.mcp.json` or `~/.claude.json`; Gemini uses
`settings.json`; Devin uses `hooks.v1.json` (machine: `config.json#hooks`) and
`mcp_config.json`; Codex uses `.codex/hooks.json` and `.codex/config.toml`.
Windsurf has no subagents. The
[output paths reference](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/reference/harnesses-and-output-paths.md)
lists every kind per harness.

Default `symlink` mode links skills, commands, and subagents into
`~/.cache/agent-marketplace/compiled/<harness>/`; `materialization.mode: file`
writes regular trees for every harness. Commit config and locks; ignore the
generated roots you use (for example `.claude/skills/`,
`.claude/.claude-plugin/`, `.devin/`).

### Shared aggregate ownership

Sync edits shared files entry by entry and keeps handwritten entries, comments,
and unrelated settings.

- Claude, Factory, Cursor, Windsurf, and Gemini hooks mark entries with
  `"_managed_by": "agent-marketplace"`.
- Devin, Codex, and Gemini MCP carry no marker. Ownership lives under
  `$XDG_STATE_HOME/agent-marketplace` (default `~/.local/state/agent-marketplace`).
  When that state is missing or corrupt, sync preserves every entry, refuses
  collisions, and prunes nothing. A raw safety backup (five newest per file) is
  taken before each mutation, even with backups disabled.
- Codex repo changes print `materialize.codex.trust-review`; the CLI never
  edits Codex trust or `.gitignore`. `CODEX_HOME` must be absolute and moves
  only the `.codex` partition.

## Locks

Each layer's lock records the exact Git commit and inventory for the sources
that layer declares, plus verified Marketplace revisions. Directory sources are
never locked. The CLI writes `agent-marketplace-lock/v2` (read by v4.0.9+) and
reads v1. The organization lock may be absent permanently; only
`lock --scope organization` writes it. A current lock does not move when the
branch moves: use `update` or `lock --upgrade`. Never hand-edit a lock.

## Daily commands

```bash
agent-marketplace list [--kind skill|command|subagent|hook|mcp|collection]
agent-marketplace list --from <git-or-web-url> [--ref R] [--path P]  # ad hoc, no config write
agent-marketplace install <name> [--kind K] [--scope repo|user|local]
agent-marketplace install <collection>
agent-marketplace install --tag <tag> [--kind K]       # repo scope only
agent-marketplace install --all [--kind K] [--registry URL]  # repo scope only
agent-marketplace install --from <url> [<name>] [--ref R] [--path P]
agent-marketplace install <marketplace-endpoint>       # registers endpoint (user scope)
agent-marketplace install alias:registry/kind/name[@sha256:...] [--as local-name]
agent-marketplace search [query] [--marketplace alias|url|all]
agent-marketplace remove <name> [--kind K] [--scope repo|user|local]
agent-marketplace remove --from <url> [<name>] [--ref R] [--path P]
agent-marketplace sync                 # materialize; reconcile stale writable locks
agent-marketplace sync --locked        # CI: require fresh locks, write no config/locks
agent-marketplace sync --check         # exit 1 on lock or output drift; writes nothing
agent-marketplace sync --offline       # trusted caches only
agent-marketplace sync --strict -v     # missing/unloadable selections become errors
agent-marketplace status               # targets, locks, synced resources, drift
agent-marketplace update [--scope S] [--registry URL]   # advance Git locks, then sync
agent-marketplace lock [--check|--upgrade] [--scope S]
agent-marketplace migrate [--scope all|user|repo|local] [--dry-run]
agent-marketplace convert --from claude --to codex --out ./out ./plugin
agent-marketplace doctor [--repair [--yes]]
agent-marketplace gc --dry-run         # cache cleanup sees only the current repo
agent-marketplace backup create|list|restore --target <harness>
agent-marketplace deinit               # remove managed artifacts and cache
```

- `remove` drops an explicit selection, or records an exclusion when a
  collection, tag, or `all` selected the resource. Required resources cannot
  be removed. When the same resource is enabled at machine and repo scope, an
  unscoped `remove` reports the ambiguity; pass `--scope`. An explicit scope
  never falls back to another, and repo/local scope cannot hide machine
  resources.
- `--kind` disambiguates a name used by more than one kind.
- `update --bump` rewrites refs only in legacy v2/v3 `registries:`; for v4,
  edit `ref:` and run `sync`.
- Read-only commands (`sync --check/--locked/--offline`, `lock --check`,
  `list`, `status`) fail with `dependency.recovery-required` after an
  interrupted write; a plain `sync` or `lock` completes the recovery.

### `doctor` and `doctor --repair`

`doctor` reports repo detection, config diagnostics, harness roots, unmet
prerequisites, and Devin CLI compatibility (3000.3 floor; it only recommends
adding `devin`). `--repair` covers Devin and Codex aggregate ownership: it
previews exact adoption matches, evidence-backed orphans, and value conflicts.
`--yes` adopts byte-identical matches only; removing an orphan or replacing a
conflict always needs an interactive per-entry confirmation. Off a terminal,
`--repair` without `--yes` only previews.

## Hosted Marketplaces

Register an endpoint with `install <url>` (adds a `marketplace` source), then
install by locator. Installation resolves and verifies the exact revision,
records it in the owning layer's lock, and prompts before accepting security
findings (`--accept-risk`/`--yes` in automation). A Marketplace group selects
through explicit `include` only; `tags`, `all`, and group `harnesses` fail at
run time with `v4.marketplace-runtime-unsupported`. A missing or mismatched lock
record fails with `v4.marketplace-lock-required`/`-mismatch`.

## Resource prerequisites

Resources declare prerequisites in their own frontmatter
(`metadata.requires`), never in `.agents/config.yaml`. They are advisory by
default: a prerequisite is met when a resource of that kind and name is
selected from any source or layer, unmet prerequisites warn during `install`,
`sync`, `sync --check`, and `doctor` and appear under `UNMET REQUIREMENTS` in
`status`, and they never override an exclude or fail `sync --check` — only
`sync --strict` (including `sync --strict --check`) exits nonzero. Setting
`requires.expand: true` in a v4 layer opts in to selecting prerequisites
transitively from already-configured sources; see
`site/docs/reference/resource-prerequisites.md` for the reference.

## Migrating legacy configs

`migrate` converts representable v2/v3 user, repo, and personal configs to v4
in one transaction and changes nothing when it refuses (include trees, lossy
input, Marketplace entries without verified cache evidence). It never edits the
organization file or uses the network. Interactive commands offer to migrate
only the user config; migrate repo and personal configs explicitly:

```bash
agent-marketplace migrate --scope repo --dry-run
agent-marketplace migrate --scope repo
agent-marketplace sync
```

`--to v2` selects the legacy structural-remediation flow.

## Gotchas

- **No selection, no output**: a source without an `include`, `tags`, or `all`
  selection only contributes to `list`.
- **No target, no output**: missing `.devin/` or `.codex/` output means the
  harness is not in the effective list (`status` → `TARGET HARNESS SET`).
- **Name collisions pick one winner**: same kind and name from different
  sources resolves by `required`, then layer/file precedence, then
  alphabetical normalized origin, with a warning. Hooks are the exception: all
  are installed. A skill and a command may share a name.
- **Collections cannot add registries**: a member naming another registry needs
  that registry configured as a source first.
- **Private Git sources**: prefer SSH URLs; HTTPS fails when terminal prompts
  are disabled.
- **`gc` sees only the current repository**: it can delete a cache entry another
  repo needs; run `--dry-run` first.
- **Keep `~/.local/state/agent-marketplace`**: it is the ownership authority,
  not a cache. Deleting it stops sync from pruning what it wrote; use
  `doctor --repair` to re-adopt.
