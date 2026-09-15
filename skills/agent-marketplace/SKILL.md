---
name: agent-marketplace
description: >
  How to use the agent-marketplace CLI to install shared AI agent resources
  from Git-based or local registries and materialize them into native harness
  layouts. Use when setting up a repo, installing/removing skills, commands,
  subagents, hooks, or MCP servers, checking sync state, remediating config
  structure, or updating pinned registry refs.
metadata:
  tags:
    - cli
    - skills
    - setup
---

# agent-marketplace

## Quick Start

The project config may declare its target harness set. If omitted, targets fall back to `~/.agents/config.yaml` and then `/etc/agent-marketplace/config.yaml`.

```bash
mkdir -p ~/.agents .agents

# Machine-wide target harness set
cat > ~/.agents/config.yaml << 'EOF'
apiVersion: agent-marketplace/v2
harnesses: [factory]
registries: []
EOF

# Repo config selects resources from registries
cat > .agents/config.yaml << 'EOF'
apiVersion: agent-marketplace/v2
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

The canonical v2 schema materializes native harness files instead of the old default `.agents/skills/` symlink tree, and supports skills, commands, subagents, hooks, and MCP servers. Configuration is the source of selection policy and generated locks provide exact Git acquisition; standalone artifacts are recreated by `agent-marketplace sync`, while shared native files are reconciled entry by entry according to the target's marker or external ownership policy. Keep generated harness output out of source control:

```gitignore
.agents/skills/
.factory/
.claude/
.cursor/
.codex/
.gemini/
.windsurf/
.devin/
```

## What the Native Schema Provides

- **Layered target harness set** — project-level `harnesses:` takes precedence; when absent, targets fall back to `~/.agents/config.yaml` and `/etc/agent-marketplace/config.yaml`.
- **Native harness targets** — set `harnesses: [factory, claude, cursor, windsurf, gemini, devin, codex]` to compile resources into each harness' layout.
- **Multi-resource selection** — select `skills`, `commands`, `subagents`, `hooks`, and `mcp` entries from the same registry catalog.
- **Source harness ingestion** — use `source_harness: claude` when autodetection is not enough; registries are ingested through `harness-compilation`.
- **Repo and machine scope outputs** — repo config materializes under repo roots like `.factory/`; global config materializes under machine roots like `~/.factory/`.
- **Dry-run drift reporting** — `status` and `sync --check` report the same native files that `sync` writes or prunes.
- **Plan-before-backup sync** — a writing `sync` builds one no-write plan for resolved and stale output, then derives configured machine-scope backup coverage from that plan before execution; stale-only removals are covered, while configured archives remain out of repo scope.
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
- **Codex** — `.agents/skills/<skill>/SKILL.md` plus `.codex/` subagents and shared configuration

## Codex

Select `codex` in the TUI harness modal (`H`) or add it to the effective
`harnesses:` list. Existing configs and `migrate` do not opt in automatically.

| Scope | Skill/command root | Subagent/hook/MCP root |
|---|---|---|
| Repo | `<repo>/.agents` | `<repo>/.codex` |
| Machine | `~/.agents` | absolute `$CODEX_HOME`, otherwise `~/.codex` |

In default symlink mode, skills and converted commands are directory symlinks
at `skills/<name>/`; file mode writes regular directory trees there. Each
contains `SKILL.md`, and commands carry `x-agent-marketplace-kind: command`.
Subagents go to `agents/<name>.toml`, hooks to `hooks.json`, and MCP servers to
`config.toml` under the Codex root.
`CODEX_HOME` never relocates the `.agents` partition.

Codex is a full conversion target (`convert --from/--to codex`,
`source_harness: codex`). One-shot `convert --to codex` is a non-owning,
staged publication and requires a destination without pre-existing
`.codex/hooks.json` or `.codex/config.toml`; choose a new or empty destination
rather than overwriting shared configuration. Autodetection requires `.codex`;
the shared `.agents/skills` layout alone is not Codex-specific. Rules,
`AGENTS.md` generation, and emitted legacy `.codex/prompts` are excluded.

`hooks.json` and `config.toml` are reconciled entry by entry through durable
ownership state and mandatory backups. Missing or corrupt state preserves every
existing entry, refuses collisions, and prunes nothing. A repo mutation may
require Codex project trust review; Agent Marketplace reports it but never
changes trust state or adds Codex output paths to `.gitignore`. It never
installs, requires, or probes a Codex executable.

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

Rules are not a selectable kind and are unsupported by this target. The links shown above are the default; `materialization.mode: file` installs regular skill/command trees and subagent files through the same generic materializer used by other targets. During sync, shared hooks and MCP configuration retain ownership-safe reconciliation in both modes.

Devin is also a full conversion target: `convert --from devin`, `convert --to devin`, and `source_harness: devin` all work. One-shot conversion is non-owning and staged, and refuses existing shared destinations. Autodetection prefers a `.devin` workspace marker over compatibility layouts; a direct user-shaped root (`skills/`, `agents/`, `config.json`, `mcp_config.json` at the top level) is never autodetected and needs an explicit `--from devin`.

### Shared Devin files

`hooks.v1.json`, `config.json`, and `mcp_config.json` are shared with whatever you write by hand, so `sync` reconciles them entry by entry:

- Handwritten entries, their comments, key order, and relative order are preserved. Handwritten hooks run before generated ones.
- No `_managed_by` marker is ever written into a Devin file. Ownership lives in durable state under `$XDG_STATE_HOME/agent-marketplace` (default `~/.local/state/agent-marketplace`), outside the repo and outside the cache — deleting the cache has no ownership effect.
- If that state is missing or corrupt, every existing entry is treated as unmanaged: `sync` preserves everything, refuses collisions, and prunes nothing. It fails conservative, never destructive.
- A raw-byte safety backup is always taken before an aggregate is mutated, even when configured backups are disabled; the five newest per file are kept.
- Updates are atomic per file: a malformed `mcp_config.json` blocks only that file and is left exactly as found while skills, subagents, and a valid hooks file still materialize.

## Git dependency locks

Remote Git registries resolve into generated locks owned by the organization, user, repository, or personal-local config layer. Commit `.agents/lock.yaml` with repository config and keep `.agents/lock.local.yaml` ignored. Normal sync retains compatible exact commits and reconciles writable stale locks; locked/check modes do not write. Plain directory registries remain live and unlocked. This Git foundation does not provide hosted Marketplace lock records or acquisition adapters.

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
agent-marketplace lock                         # Create/reconcile generated Git locks
agent-marketplace lock --upgrade               # Advance Git requirements to current refs
agent-marketplace sync                         # Materialize from exact Git snapshots
agent-marketplace sync --locked                # Require fresh locks; write no config or locks
agent-marketplace sync --check                 # Check lock and output drift without writing
agent-marketplace status                       # Show exact locks, HARNESS TARGETS, refs, cache, drift
agent-marketplace update                       # Advance Git locks and synchronize
agent-marketplace update --bump                # Rewrite eligible refs before advancing locks
agent-marketplace migrate --harnesses factory  # Remediate structural config issues
agent-marketplace convert --from claude --to factory --out ./out ./plugin
agent-marketplace doctor                       # Harness/root diagnostics + Devin CLI detection
agent-marketplace doctor --repair              # Preview Devin/Codex aggregate ownership recovery
agent-marketplace doctor --repair --yes        # Adopt exact matches without prompting
agent-marketplace tui                          # Browse and edit selections interactively (H = harnesses)
```

For `remove --from`, pass the registry URL and use the entry's authored
`--path` spelling to narrow a named removal to a configured subdirectory and
its discovered descendants. Omitting `--path` or `--ref` is only unambiguous
when the URL identifies one logical configured location; distinct paths or
refs require the corresponding flag. Use `--path` (and `--ref` when needed)
for whole-registry removal when one URL has multiple entries. Equivalent
normalized URL/path spellings identify the same entry. `--scope` chooses the
config root and defaults to `repo` for `--from`; any diagnostic `@ref` suffix is
only a label, not part of the URL—pass the ref separately when targeting it.

`status` prints a **HARNESS TARGETS** table: one row per effective harness with its capabilities (`compile`, `ingest`, `convert`, `tui`) and both its repo and machine roots. A harness name the CLI does not recognize is still listed, so a typo shows up instead of being silently dropped.

## `doctor` and `doctor --repair`

Plain `doctor` reports the repo root, the autodetected harness, and every install root with its declared capabilities and both roots. It also:

- detects a locally installed Devin CLI and prints its version and path;
- warns when that version is below the 3000.3 floor, or when the version cannot be established. Neither warning blocks anything — `sync` never needs a Devin binary;
- recommends adding `devin` to a harnesses list when a Devin CLI is installed but no layer selects it. It only recommends; it never edits a harness list.

`--repair` is the recovery surface for Devin and Codex shared-aggregate
ownership state, and nothing else:

- It previews three classes: **exact adoption matches** (entries already byte-identical to what the marketplace would write), **evidence-backed orphan candidates** (entries a retained safety backup or journal ties to earlier managed output), and **value conflicts** (an unmanaged entry has the same key as a selected resource but a different value). Orphans without evidence and unrelated unmanaged entries are never nominated — edit those yourself.
- On a terminal it then prompts per action. Off a terminal it is **preview-only** and mutates nothing.
- `--yes` adopts exact matches without prompting. Adoption changes no bytes of the file; it only records that an already-identical entry is managed.
- `--yes` **never** removes an orphan or replaces a conflicting entry. Both destructive actions always need an interactive per-entry confirmation and take a fresh safety backup first.
- Exit `0` = clean, repaired, or declined. Exit `1` = corrupt ownership state, an unparseable aggregate file, or a partial repair failure.
- `--yes` without `--repair` is an error.

## Config Reference

```yaml
# .agents/config.yaml (committed repo config)
apiVersion: agent-marketplace/v2

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

- **`apiVersion`** should be `agent-marketplace/v2`; `agent-marketplace/v3` remains accepted as a compatibility alias.
- **`harnesses`** may be declared in project or user configuration. Project values take precedence over user and organization defaults. Valid values include `factory`, `claude`, `cursor`, `windsurf`, `gemini`, `devin`, and `codex`.
- **`source_harness`** is optional per registry entry; empty means autodetect.
- **`skills`**, **`commands`**, **`subagents`**, **`hooks`**, **`mcp`**, **`collections`**, and **`tags`** select what materializes via `KindSelector` (`include`/`exclude` lists or a shorthand sequence treated as `include`).
- **Per-registry selections** override top-level `skills`, `commands`, `subagents`, `hooks`, `mcp`, and `tags`; per-entry `harnesses` narrows the effective target set.
- **Nested collections** are resolved from configured registry ownership. The legacy `skills:` member list and typed `resources:` list are both supported and merged. Once selected, the nested owner's URL, path, ref, and required policy flow to its recursively expanded members. Its effective harness targets inherit from the containing collection unless the owner explicitly narrows them; it cannot widen the parent's targets, and an explicit empty list opts out. Shared nested collections are valid across sibling include branches; only a repeated ancestor on one branch is a cycle.
- **`--kind`** disambiguates names that exist in multiple resource kinds and selects non-skill resources for `install --all` or `install --tag`.
- **Local registries** can use `/absolute`, `~/home-relative`, or `./relative` paths with no `ref`. Relative paths must remain inside the directory containing the local config.

### Global config: `~/.agents/config.yaml`

```yaml
apiVersion: agent-marketplace/v2
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
- **`default: true`** resources auto-select; higher-precedence config can exclude repo-scoped resources, while repo and local excludes cannot suppress machine-scoped resources.

### Local config: `.agents/config.local.yaml`

```yaml
skills:
  include: [my-personal-skill]
  exclude: [some-skill-you-dont-want]
```

Local config is gitignored and per-checkout. Its excludes apply only to
non-required repo-scoped resources; machine-scoped resources remain immune.
`sync` adds `.agents/config.local.yaml` to `.gitignore` when the file exists.

When the same kind and name is enabled at machine and repo scope, an unscoped
`remove` reports the ambiguity instead of choosing a copy. Use
`remove --scope user` or `remove --scope repo` to remove only that occurrence;
the other scope remains enabled. An explicit scope never falls back to another
scope when that scope has no occurrence; it reports that the resource is not
enabled, while a machine-owned occurrence targeted from repo/local scope
produces a boundary error. `--scope local` targets checkout-local
selections/excludes and cannot bypass the machine/repo boundary. Required
resources cannot be removed from any scope.

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

## Config Remediation

v1 configs (no `apiVersion`) are rejected by the loader. v2 is the canonical
schema and v3 is accepted as a compatibility alias. `migrate` is reserved for
structural remediation and does not rewrite valid v2 or v3 files:

```bash
agent-marketplace migrate --harnesses factory,cursor
agent-marketplace sync
```

Then replace old `.agents/skills/` ignore rules with the harness output roots
you use. Keep `.agents/skills/` ignored when `codex` is selected, unless your
repo intentionally tracks generated harness files.

## Gotchas

- **No selection means no output** — a registry with no kind-specific selections, `tags`, `collections`, `required`, or `default` only contributes to `list`.
- **Name collisions are resolved deterministically** — same-kind resources from different registries produce a warning; the resolver chooses one winner using required policy, config precedence, normalized URL, and stable order. Same-URL+ref parent/child paths are merged as one physical source, with the deeper path winning overlapping names; non-overlapping same-scope paths remain visible but only one can materialize for that target.
- **Collision labels may include refs** — when the same URL/path appears at multiple refs, warnings append `@<ref>` to non-empty refs to distinguish participants. The remediation command uses the URL and, when needed, an explicit `--path` rather than copying that display suffix.
- **Ambiguous cross-kind names need `--kind`** — if a skill and command share a name, rerun `install` or `remove` with `--kind`.
- **Prerequisites are advisory** — unmet prerequisites never change selection, and explicit excludes remain in force. Resources from a `required: true` registry cannot be excluded, so they satisfy matching prerequisites.
- **Collection-level `requires` is ignored** — collections expand into members before prerequisite validation.
- **Private registries should use SSH URLs** — HTTPS can fail when terminal prompts are disabled.
- **Cache keys are URL-dependent** — changing the registry URL creates a separate cache entry.
- **No target selection means no target output** — if you expected `.devin/` or `.codex/` output and got none, check that the target is in the effective `harnesses:` list (`status` → HARNESS TARGETS).
- **Devin file mode writes regular trees** — `materialization.mode: file` writes regular Devin skill/command trees and subagent files, while shared hooks and MCP aggregates retain ownership-safe reconciliation.
- **`CODEX_HOME` moves only Codex configuration** — it must be absolute; machine skills and converted commands remain under `~/.agents/skills`.
- **Do not delete `~/.local/state/agent-marketplace` to "reset" Devin or Codex** — it is the ownership authority, not a cache. Without it every existing aggregate entry becomes unmanaged, so `sync` stops pruning what it previously wrote. Use `doctor --repair` to re-adopt instead.
