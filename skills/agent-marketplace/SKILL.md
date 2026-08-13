---
name: agent-marketplace
description: >
  How to use the agent-marketplace CLI to install shared AI agent resources
  from Git-based or local registries and materialize them into native harness
  layouts. Use when setting up a repo, installing/removing skills, commands,
  subagents, hooks, or MCP servers, checking sync state, migrating v2 configs,
  or updating pinned registry refs.
metadata:
  tags:
    - cli
    - skills
    - setup
---

# agent-marketplace

## Quick Start

The target harness set is machine-wide (`~/.agents/config.yaml` or `/etc/agent-marketplace/config.yaml`). Repo configs only select resources.

```bash
mkdir -p ~/.agents .agents

# Machine-wide target harness set
cat > ~/.agents/config.yaml << 'EOF'
apiVersion: agent-marketplace/v3
harnesses: [factory]
registries: []
EOF

# Repo config selects resources from registries
cat > .agents/config.yaml << 'EOF'
apiVersion: agent-marketplace/v3
registries:
  - url: git@gitlab.example.com:example-user/infra-skills.git
    ref: main
    source_harness: claude
    path: skills
    skills:
      include: [vm-lifecycle, enc-debugging]
EOF

agent-marketplace sync
agent-marketplace status
```

v3 materializes native harness files instead of the old default `.agents/skills/` symlink tree, and extends the same flow beyond skills to commands, subagents, hooks, and MCP servers. Configuration is the source of truth; native harness output is generated, disposable, and recreated by `agent-marketplace sync`. Keep generated harness output out of source control:

```gitignore
.factory/
.claude/
.cursor/
.gemini/
.windsurf/
.devin/
```

## What v3 Adds

- **Machine-wide target harness set** — `harnesses:` lives in `~/.agents/config.yaml` or `/etc/agent-marketplace/config.yaml` and is the source of truth for the machine. Repo and local configs must not declare a top-level `harnesses:`.
- **Native harness targets** — set `harnesses: [factory, claude, cursor, windsurf, gemini, devin]` to compile resources into each harness' layout.
- **Multi-resource selection** — select `skills`, `commands`, `subagents`, `hooks`, and `mcp` entries from the same registry catalog.
- **Source harness ingestion** — use `source_harness: claude` when autodetection is not enough; registries are ingested through `harness-compilation`.
- **Repo and machine scope outputs** — repo config materializes under repo roots like `.factory/`; global config materializes under machine roots like `~/.factory/`.
- **Dry-run drift reporting** — `status` and `sync --check` report the same native files that `sync` writes or prunes.
- **Resource prerequisites** — resources declare `metadata.requires`; unmet prerequisites produce warnings but are never installed automatically.
- **Conversion and diagnostics** — `convert` translates one harness layout to another; `doctor` shows harness/root diagnostics.

Typical native outputs:

- **Factory Droid** — `.factory/skills/<skill>/SKILL.md`
- **Factory Droid commands** — `.factory/commands/<command>.md`
- **Claude Code** — `.claude/skills/<skill>/<skill>.md`
- **Cursor** — `.cursor/rules/<skill>.md`
- **Gemini CLI** — `.gemini/skills/<skill>/SKILL.md`
- **Windsurf** — files under `.windsurf/`
- **Devin CLI** — `.devin/skills/<skill>/SKILL.md` (repo) and `~/.config/devin/skills/<skill>/SKILL.md` (machine)

## Devin

Select `devin` like any other harness target: press `H` in the TUI and tick **Devin CLI**, or add `devin` to a `harnesses:` list.

Roots are `<repo>/.devin` and `~/.config/devin`. Outputs:

| Kind | Devin output |
|---|---|
| Skill | `skills/<name>/SKILL.md` (directory symlink) |
| Command | `skills/<name>/SKILL.md` with `x-agent-marketplace-kind: command` |
| Subagent | `agents/<name>.md` (file symlink) |
| Hook | repo `hooks.v1.json`; machine `config.json` → `hooks` member |
| MCP server | `mcp_config.json` (same relative path in both scopes) |

Rules are not a selectable kind and are unsupported by this target. Devin is symlink-only: `materialization.mode: file` is refused for it (`materialize.devin.symlink-required`) instead of falling back to regular files.

Devin is also a full conversion target: `convert --from devin`, `convert --to devin`, and `source_harness: devin` all work. Autodetection prefers a `.devin` workspace marker over compatibility layouts; a direct user-shaped root (`skills/`, `agents/`, `config.json`, `mcp_config.json` at the top level) is never autodetected and needs an explicit `--from devin`.

### Shared Devin files

`hooks.v1.json`, `config.json`, and `mcp_config.json` are shared with whatever you write by hand, so `sync` reconciles them entry by entry:

- Handwritten entries, their comments, key order, and relative order are preserved. Handwritten hooks run before generated ones.
- No `_managed_by` marker is ever written into a Devin file. Ownership lives in durable state under `$XDG_STATE_HOME/agent-marketplace` (default `~/.local/state/agent-marketplace`), outside the repo and outside the cache — deleting the cache has no ownership effect.
- If that state is missing or corrupt, every existing entry is treated as unmanaged: `sync` preserves everything, refuses collisions, and prunes nothing. It fails conservative, never destructive.
- A raw-byte safety backup is always taken before an aggregate is mutated, even when configured backups are disabled; the five newest per file are kept.
- Updates are atomic per file: a malformed `mcp_config.json` blocks only that file and is left exactly as found while skills, subagents, and a valid hooks file still materialize.

## Daily Commands

```bash
agent-marketplace list                         # Browse resources and collections
agent-marketplace list --kind command          # Browse commands, subagents, hooks, or mcp
agent-marketplace install <skill>              # Add a skill and sync
agent-marketplace install --kind command X     # Add a non-skill resource and sync
agent-marketplace install <collection>         # Add a collection and sync
agent-marketplace install --tag <tag>          # Install resources matching a tag
agent-marketplace install --tag <tag> --kind hook
agent-marketplace install --all                # Install every discovered skill by default
agent-marketplace install --all --kind mcp     # Install every discovered MCP server
agent-marketplace remove <resource|collection> # Remove selection or add an exclude
agent-marketplace remove --kind subagent X     # Disambiguate a resource kind
agent-marketplace sync                         # Materialize native harness outputs
agent-marketplace sync --check                 # Check drift without writing
agent-marketplace status                       # Show resources, HARNESS TARGETS, refs, cache, drift
agent-marketplace update                       # Refresh registry caches
agent-marketplace update --bump                # Bump pinned refs when newer refs exist
agent-marketplace migrate --harnesses factory  # Rewrite v2 configs to v3
agent-marketplace convert --from claude --to factory --out ./out ./plugin
agent-marketplace doctor                       # Harness/root diagnostics + Devin CLI detection
agent-marketplace doctor --repair              # Preview Devin aggregate ownership recovery
agent-marketplace doctor --repair --yes        # Adopt exact matches without prompting
agent-marketplace tui                          # Browse and edit selections interactively (H = harnesses)
```

`status` prints a **HARNESS TARGETS** table: one row per effective harness with its capabilities (`compile`, `ingest`, `convert`, `tui`) and both its repo and machine roots. A harness name the CLI does not recognize is still listed, so a typo shows up instead of being silently dropped.

## `doctor` and `doctor --repair`

Plain `doctor` reports the repo root, the autodetected harness, and every install root with its declared capabilities and both roots. It also:

- detects a locally installed Devin CLI and prints its version and path;
- warns when that version is below the 3000.3 floor, or when the version cannot be established. Neither warning blocks anything — `sync` never needs a Devin binary;
- recommends adding `devin` to a harnesses list when a Devin CLI is installed but no layer selects it. It only recommends; it never edits a harness list.

`--repair` is the recovery surface for Devin's shared-aggregate ownership state, and nothing else:

- It previews three classes: **exact adoption matches** (entries already byte-identical to what the marketplace would write), **evidence-backed orphan candidates** (entries a retained safety backup or journal ties to earlier managed output), and **value conflicts** (an unmanaged entry has the same key as a selected resource but a different value). Orphans without evidence and unrelated unmanaged entries are never nominated — edit those yourself.
- On a terminal it then prompts per action. Off a terminal it is **preview-only** and mutates nothing.
- `--yes` adopts exact matches without prompting. Adoption changes no bytes of the file; it only records that an already-identical entry is managed.
- `--yes` **never** removes an orphan or replaces a conflicting entry. Both destructive actions always need an interactive per-entry confirmation and take a fresh safety backup first.
- Exit `0` = clean, repaired, or declined. Exit `1` = corrupt ownership state, an unparseable aggregate file, or a partial repair failure.
- `--yes` without `--repair` is an error.

## Config Reference

```yaml
# .agents/config.yaml (committed repo config — no top-level harnesses)
apiVersion: agent-marketplace/v3

registries:
  - url: git@gitlab.example.com:example-user/infra-skills.git
    ref: main
    source_harness: claude
    path: skills

skills:
  include: [vm-lifecycle]

commands:
  include: [deploy-service]

subagents:
  include: [incident-responder]

hooks:
  include: [pre-tool-audit]

mcp:
  include: [service-catalog]

collections:
  include: [platform-onboarding]

tags:
  - puppet
```

- **`apiVersion`** must be `agent-marketplace/v3`.
- **`harnesses`** is machine-wide and belongs in `~/.agents/config.yaml` or `/etc/agent-marketplace/config.yaml`; repo and local configs must not declare it. Valid values include `factory`, `claude`, `cursor`, `windsurf`, `gemini`, and `devin`.
- **`source_harness`** is optional per registry entry; empty means autodetect.
- **`skills`**, **`commands`**, **`subagents`**, **`hooks`**, **`mcp`**, **`collections`**, and **`tags`** select what materializes via `KindSelector` (`include`/`exclude` lists or a shorthand sequence treated as `include`).
- **Per-registry selections** override top-level `skills`, `commands`, `subagents`, `hooks`, `mcp`, `tags`, and per-entry `harnesses` narrows the machine-wide target set.
- **`--kind`** disambiguates names that exist in multiple resource kinds and selects non-skill resources for `install --all` or `install --tag`.
- **Local registries** can use `/absolute`, `~/home-relative`, or `./relative` paths with no `ref`. Relative paths must remain inside the directory containing the local config.

### Global config: `~/.agents/config.yaml`

```yaml
apiVersion: agent-marketplace/v3
harnesses: [factory]
registries:
  - url: git@gitlab.example.com:example-org/example-security-skills.git
    ref: main
    required: true

  - url: git@gitlab.example.com:example-org/example-shared-skills.git
    ref: main
    default: true
```

- **`required: true`** resources always materialize and cannot be excluded.
- **`default: true`** resources auto-select but can be excluded by higher-precedence config.

### Local config: `.agents/config.local.yaml`

```yaml
skills:
  include: [my-personal-skill]
  exclude: [some-skill-you-dont-want]
```

Local config is gitignored and per-checkout. `sync` adds `.agents/config.local.yaml` to `.gitignore` when the file exists.

## Resource Prerequisites

Registry resources can declare advisory prerequisites in their own
frontmatter. Do not put `metadata.requires` in `.agents/config.yaml`:

```yaml
---
name: smoke-test-kb
description: Answer smoke test questions from Confluence
metadata:
  requires:
    - mcp:genai-atlassian
    - name: vm-lifecycle
      kind: skill
      registry: git@gitlab.example.com:example-user/infra-skills.git
      path: skills
---
```

Use `"<kind>:<name>"` shorthand, or a map with required `name` and
optional `kind`, `registry`, and `path`. A bare `"name"` or an omitted
`kind` means `skill`. Supported kinds are `skill`, `command`,
`subagent`, `hook`, and `mcp`; `collection` is not supported.

Prerequisites are satisfied by selected kind and name. `registry` and
`path` only refine the suggested install command. They do not constrain
which selected resource satisfies the prerequisite.

An unmet prerequisite is advisory: agent-marketplace never selects,
installs, or removes an exclude for it. Warnings appear during
`install`, `sync`, `sync --check`, and `doctor`; `status` lists them
under `UNMET REQUIREMENTS`. Prerequisite warnings alone do not make
`sync --check` fail:

```text
warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected
  → agent-marketplace install "genai-atlassian" --kind mcp
```

## Legacy v2 Migration

v1 configs (no `apiVersion`) are rejected by the loader. If you have a v2 config, `migrate` rewrites it to v3 and moves the machine-wide `harnesses:` list to `~/.agents/config.yaml`:

```bash
agent-marketplace migrate --harnesses factory,cursor
agent-marketplace sync
```

Then replace old `.agents/skills/` ignore rules with the harness output roots you use, unless your repo intentionally tracks generated harness files.

## Gotchas

- **No selection means no output** — a registry with no kind-specific selections, `tags`, `collections`, `required`, or `default` only contributes to `list`.
- **Name collisions fail sync** — same-kind resources from multiple registries fail; resolve by narrowing per-registry selections, adding `exclude`, or removing one source.
- **Ambiguous cross-kind names need `--kind`** — if a skill and command share a name, rerun `install` or `remove` with `--kind`.
- **Prerequisites are advisory** — unmet prerequisites never change selection, and explicit excludes remain in force. Resources from a `required: true` registry cannot be excluded, so they satisfy matching prerequisites.
- **Collection-level `requires` is ignored** — collections expand into members before prerequisite validation.
- **Private registries should use SSH URLs** — HTTPS can fail when terminal prompts are disabled.
- **Cache keys are URL-dependent** — changing the registry URL creates a separate cache entry.
- **No target selection means no target output** — if you expected `.devin/` output and got none, check that `devin` is in the effective `harnesses:` list (`status` → HARNESS TARGETS).
- **Devin needs symlinks** — with `materialization.mode: file` the Devin target is refused outright rather than writing regular files. Other targets in the same run are unaffected.
- **Do not delete `~/.local/state/agent-marketplace` to "reset" Devin** — it is the ownership authority, not a cache. Without it every existing aggregate entry becomes unmanaged, so `sync` stops pruning what it previously wrote. Use `doctor --repair` to re-adopt instead.
