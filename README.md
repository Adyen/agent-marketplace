# agent-marketplace

`agent-marketplace` is a single Go binary that distributes shared AI agent
resources — skills, commands, subagents, and MCP servers — from **registries**
into the native layout of whichever AI coding harness you use (Factory Droid,
Claude Code, Cursor, Windsurf, Gemini CLI, and Devin CLI).

A **registry** is just a Git repository (or a local directory) that holds those
resources. You declare which registries you want, which resources to pull from
each, and which harnesses to target. The CLI clones the registries, compiles
the resources for your harness, and writes the result to disk. Your config is
the source of truth; the generated harness files are disposable and can be
re-created at any time with a single `sync`.

> Every resource you enable is always tied to a specific registry — identified
> by its **URL**, **ref**, and (optionally) **path**. There is no global,
> registry-less "skill" or "command": everything is namespaced to where it
> comes from. This keeps installs reproducible and makes it obvious which
> source a resource was materialized from.

---

## Installation

### Via a package manager

`agent-marketplace` is published to package managers and GitHub releases. Install the released binary for your platform.

### Build from source

Requires **Go 1.26+**.

```bash
# Build the local binary (./agent-marketplace)
make build

# Cross-compile versioned release artifacts for
# darwin-arm64, darwin-amd64, and linux-amd64 into ./dist/
make release
```

Use `make build` for day-to-day local use, and `make release` when you need
the distributable, version-stamped artifacts (the same ones shipped through
package managers and GitHub releases).

---

## Two ways to use it

| Mode | When to use it | How to launch |
|------|----------------|---------------|
| **TUI** (interactive) | Browsing, discovering, and toggling resources by hand | run `agent-marketplace` with no arguments on a terminal, or `agent-marketplace tui` |
| **CLI** (commands) | Scripted/repeatable installs, CI checks, automation | `agent-marketplace <command> ...` |

Both modes operate on the same config files and produce the same result — the
TUI is a visual front-end over the commands described below.

In the TUI, the most common keys are: `j/k` to navigate, `space` to toggle a
resource, `/` to search, `s` to sync pending changes, `u` to refresh registry
caches, `r` to reload from disk, `H` to pick which harnesses to target, and `q`
to quit.

---

## Quick start

The fastest way to get value is to point at a registry and install everything
it offers (a "bundle" install), then `sync`.

```bash
# 1. See what a registry contains — nothing is written to disk yet
agent-marketplace list --from git@gitlab.example.com:example-org/infra-skills.git --ref main

# 2. Install ALL resources from that registry (bundle mode).
#    The registry is added to your repo config automatically.
agent-marketplace install --from git@gitlab.example.com:example-org/infra-skills.git --ref main

# 3. Materialize everything to your harness layout (e.g. .factory/)
agent-marketplace sync

# 4. See exactly what landed on disk, and from which registry/ref
agent-marketplace status
```

The commands above assume a machine-wide target harness set is already
configured (usually `harnesses: [factory]` in `~/.agents/config.yaml` or
`/etc/agent-marketplace/config.yaml`).

To install just one resource instead of the whole bundle, name it. Because the
name alone can be ambiguous, the registry it comes from is always recorded:

```bash
# Install a single skill from a specific registry + ref
agent-marketplace install vm-lifecycle \
  --from git@gitlab.example.com:example-org/infra-skills.git --ref main

# Pull from a subdirectory of the registry
agent-marketplace install deploy-service \
  --from git@gitlab.example.com:example-org/infra-skills.git --ref main --path commands

# Disambiguate when the same name exists in multiple kinds
agent-marketplace install deploy-service \
  --from git@gitlab.example.com:example-org/infra-skills.git --ref main --kind command
```

You can also paste a GitLab/GitHub web URL — the `ref` and `path` are inferred
from the `/-/tree/<ref>/<path>` (GitLab) or `/tree/<ref>/<path>` (GitHub)
portion of the URL:

```bash
agent-marketplace install \
  --from https://gitlab.example.com/example-org/infra-skills/-/tree/main/acs-compute-ops/skills/vm-lifecycle
```

Day-to-day operation comes down to a handful of commands:

```bash
agent-marketplace sync          # reconcile config -> disk (idempotent)
agent-marketplace sync --check  # report drift only; exits 1 if out of sync (CI/pre-commit)
agent-marketplace status        # what's installed, from where, and any drift/errors
agent-marketplace update        # pull the latest commit for each pinned ref
agent-marketplace list          # browse resources from your configured registries
```

> **`sync` is deterministic and idempotent.** The same config always produces
> the same files. Delete the generated `.factory/` directory and re-run
> `sync` — you get the identical result back. That is why `sync --check` is
> safe to wire into pre-commit hooks and CI.

---

## Configuration and precedence layers

Configuration is layered. Each layer lives in a specific file and has a fixed
purpose. Layers are merged so that **more specific layers override more general
ones**: your repo and personal configs take precedence over the user and org
layers when selecting resources, so an org can set a baseline while
individuals tailor it for their machine or checkout. The org layer's strength is
*enforcement*, not selection priority — its `required: true` resources cannot be
excluded by any layer (see below).

| Layer | File | Scope | Who controls it |
|-------|------|-------|-----------------|
| **Organization** | `/etc/agent-marketplace/config.yaml` | machine-wide | IT / MDM (enforced) |
| **User personal** | `~/.agents/config.yaml` | machine-wide (all repos) | You, per machine |
| **Repo committed** | `<repo>/.agents/config.yaml` | this repo (shared) | The team, committed to git |
| **Repo personal** | `<repo>/.agents/config.local.yaml` | this repo (just you) | You, gitignored |

How it works from a user's point of view:

- The CLI reads every layer that exists, then **merges** them. The result is one
  effective set of registries and resources. When a resource is both selected
  and excluded across layers, the more specific layer (repo, then personal) wins
  — except `required: true` resources, which no layer can exclude.
- **Machine-wide** layers materialize into your home harness root (e.g.
  `~/.factory/`); **repo** layers materialize into `<repo>/.factory/`. The two
  are complementary — a resource can exist at both scopes without conflict.
- The CLI stamps every resource with the layer it came from, which is why
  `status` can show you the exact origin (and `--scope` lets you pick which
  file an `install`/`remove` writes to).
- `sync` automatically adds `.agents/config.local.yaml` to `.gitignore` so your
  personal overrides never get committed.

A minimal committed repo config looks like this — note that the resource
selection lives **inside** the registry entry, namespaced by `url` + `ref`
(+ optional `path`). The target harness set is machine-wide
(`~/.agents/config.yaml` or `/etc/agent-marketplace/config.yaml`), so repo
configs do **not** declare a top-level `harnesses:` block:

```yaml
# .agents/config.yaml  (committed)
apiVersion: agent-marketplace/v3

registries:
  - url: git@gitlab.example.com:example-org/infra-skills.git
    ref: main
    path: skills              # subdirectory inside the registry (optional)
    source_harness: claude    # layout of the registry content (optional; autodetected)
    skills:
      include: [vm-lifecycle, k8s-basics]
    commands:
      include: [deploy-service]
    subagents:
      include: [incident-responder]
```

The full precedence and selection rules — including edge cases and error
handling — are documented in [`docs/PRECEDENCE.md`](./docs/PRECEDENCE.md).

### Monorepos and nested configs

The "repo" layer is resolved by walking **up** from your current directory and
using the **nearest** ancestor that contains `.agents/config.yaml`. This has
concrete consequences in a monorepo with configs at multiple levels:

- Only **one** repo config applies per run — the closest one above your cwd.
  Configs at different levels are **not merged** with each other.
- Generated harness output (`.factory/`, etc.) is written next to that
  nearest config, i.e. at that directory, not at the monorepo root.
- Running from a subdirectory that has **no** `.agents/config.yaml` falls back
  to the nearest ancestor config (e.g. the monorepo root), and writes output
  there.

Empirically, given:

```
mono/.agents/config.yaml                  # selects root-skill
mono/services/team-a/.agents/config.yaml  # selects team-skill
```

| Run from | Config used | Output written to |
|----------|-------------|-------------------|
| `mono/` | `mono/.agents/config.yaml` | `mono/.factory/` |
| `mono/services/team-a/` | `mono/services/team-a/.agents/config.yaml` | `mono/services/team-a/.factory/` |
| `mono/services/team-b/deep/` (no config) | `mono/.agents/config.yaml` (nearest ancestor) | `mono/.factory/` |

So in a monorepo, give each independently-managed subtree its own
`.agents/config.yaml`; that subtree then behaves as its own self-contained
repo root. (The user and org layers still apply on top, regardless of
which subtree you are in.)

---

## Selecting resources: opt-in and opt-out

Every registry entry carries its own `include`/`exclude` selectors per resource
kind. Because selection is attached to the registry, the resource is always
fully qualified by its source (`url` + `ref` + `path`).

```yaml
registries:
  - url: git@gitlab.example.com:example-org/infra-skills.git
    ref: main
    skills:
      include: [vm-lifecycle, k8s-basics]   # opt IN to these specific skills
      exclude: [legacy-vm-flow]             # opt OUT (e.g. pulled in by a tag)
    tags:
      - k8s                                 # opt in to everything tagged k8s
```

Each kind selector (always written inside a registry entry) accepts two
equivalent forms:

```yaml
# Shorthand list form -> treated as "include"
skills: [vm-lifecycle, k8s-basics]

# Map form -> explicit include and exclude
skills:
  include: [vm-lifecycle, k8s-basics]
  exclude: [legacy-vm-flow]
```

### Machine-wide opt-in with `required` and `default`

A registry entry in a machine-wide layer can mark its resources as automatically
selected:

```yaml
# ~/.agents/config.yaml  (or /etc/agent-marketplace/config.yaml for the organization layer)
registries:
  - url: git@gitlab.example.com:example-org/security-skills.git
    ref: main
    required: true     # always materialized; CANNOT be excluded at any layer

  - url: git@gitlab.example.com:example-org/shared-skills.git
    ref: main
    default: true      # auto-selected, but a higher layer can opt out
```

- **`required: true`** — mandatory; immune to every `exclude` at every layer.
  This is how org-wide security standards are enforced.
- **`default: true`** — every resource is selected automatically, but any
  higher-precedence layer (e.g. your repo-personal file) can exclude individual
  ones.
- **Neither** — resources are only selected when explicitly listed in an
  `include` or matched by `tags`.

### Personal config (without touching the repo config)

The gitignored local file (`.agents/config.local.yaml`) is your per-repo
personal layer. It supports **three** things:

1. **Top-level excludes** — suppress a non-required resource by name, no matter
   which registry provides it:

```yaml
# .agents/config.local.yaml  (gitignored — personal)
skills:
  exclude: [deprecated-legacy-skill]   # by name, across all registries
mcp:
  exclude: [noisy-mcp-server]
```

2. **Per-registry selectors** — scope an include or exclude to a specific
   registry entry:

```yaml
# .agents/config.local.yaml  (gitignored — personal)
registries:
  - url: git@gitlab.example.com:example-team/infra-skills.git
    ref: main
    skills:
      exclude: [noisy-skill]  # only from this registry
```

3. **Adding a personal registry** — add a registry just for your checkout,
   without committing it for the whole team or adding it globally:

```yaml
# .agents/config.local.yaml  (gitignored — personal)
registries:
  - url: git@gitlab.example.com:my/personal-skills.git
    ref: main
    default: true
```

```bash
agent-marketplace install --from git@gitlab.example.com:my/personal-skills.git --scope local
agent-marketplace sync   # re-materialize with your local registry included
```

Or equivalently, write the `registries:` block by hand and then `sync`.

**Note:** Top-level selectors in local config are applied *globally* (across all
registries), unlike `registries:`-scoped selectors which only affect the entry
they're on. Use `registries:`-scoped selectors when you want precise control;
use top-level `exclude` as a broad veto.

After editing any config layer by hand, run `agent-marketplace sync` to
reconcile the generated files, or `agent-marketplace sync --check` to verify
you are in sync without writing anything.

---

## Materialization mode

Materialization defaults to cache-backed `symlink` output. To produce portable
regular files and skill trees instead, set `materialization.mode: file`:

```yaml
# ~/.agents/config.yaml — default for user and project output
materialization:
  mode: file
```

A committed `.agents/config.yaml` or personal `.agents/config.local.yaml` can
set the same block to override **project-scoped** output only; it never changes
machine-scoped resources selected from the user config. File mode writes an
ownership manifest at `.agents/materialization-manifest.yaml`. It contains only
target-relative paths, is intentionally not gitignored, and is never staged or
committed by the CLI. User-scope file-mode state is kept locally under
`~/.agent-marketplace/`.

The `devin` target is symlink-only: it has no real-file fallback, so
`materialization.mode: file` is refused for that target with an error
diagnostic (`materialize.devin.symlink-required`) rather than degrading to a
layout Devin is not proven to read. Other targets in the same run are
unaffected.

---

## Devin

Devin CLI is a harness target. Select it the same way as any other target:

- in the TUI, press `H` and tick **Devin CLI**, or
- add `devin` to a `harnesses:` list:

```yaml
# ~/.agents/config.yaml
apiVersion: agent-marketplace/v2
harnesses: [factory, devin]
```

Devin writes to its canonical roots — `<repo>/.devin` for repo scope and
`~/.config/devin` for machine scope — and covers all five selectable resource
kinds:

| Resource kind | Devin output |
|---------------|--------------|
| Skill | `skills/<name>/SKILL.md` (directory symlink) |
| Command | `skills/<name>/SKILL.md`, marked `x-agent-marketplace-kind: command` |
| Subagent | `agents/<name>.md` (file symlink) |
| Hook | repo: `hooks.v1.json`; machine: the `hooks` member of `config.json` |
| MCP server | `mcp_config.json` (same relative path in both scopes) |

Rules are not a marketplace-selectable kind and are not supported by this
target; a rule produces a diagnostic instead of an artifact.

Devin is also a full **conversion** target, so `agent-marketplace convert --from
devin`, `convert --to devin`, and `source_harness: devin` on a registry entry
all work. Autodetection prefers a `.devin` workspace marker over any
compatibility layout that happens to sit at the same root. A *user-shaped* root
(one with `skills/`, `agents/`, `config.json`, and `mcp_config.json` at its top
level) is deliberately never autodetected — select it explicitly with `--from
devin`.

### Shared Devin files are never clobbered

`hooks.v1.json`, `config.json`, and `mcp_config.json` are shared files that you
may also edit by hand, so `sync` reconciles them entry by entry instead of
rewriting them:

- **Handwritten entries are never touched.** Unmanaged hooks and MCP servers are
  preserved, along with their comments, key order, and relative order.
  Handwritten hooks keep their position and run before generated ones.
- **No marker is written into a Devin file.** Unlike `mcp.json` for other
  harnesses, Devin files get no `"_managed_by"` key. Ownership is recorded in
  durable state under `$XDG_STATE_HOME/agent-marketplace` (default
  `~/.local/state/agent-marketplace`), outside the repo and outside the cache.
- **Deleting the cache is harmless**; it has no effect on ownership at all.
- **Missing or corrupt ownership state is conservative, not destructive.** Every
  existing entry is then treated as unmanaged, so `sync` preserves everything,
  refuses collisions, and prunes nothing. The worst outcome is a stale entry you
  can delete yourself — never a deleted entry you wrote.
- **A raw-byte safety backup is always taken** before a Devin aggregate is
  mutated, even when configured backups are disabled, and the five newest
  backups per file are retained.
- **Per-file atomicity.** A malformed `mcp_config.json` blocks only that file and
  is left byte-for-byte as found; skills, subagents, and a valid hooks file still
  materialize.

### `doctor --repair`

`agent-marketplace doctor --repair` is the recovery surface for that ownership
state. It only touches Devin's shared aggregate files; it never edits a
`harnesses:` list.

```bash
agent-marketplace doctor                # detection and diagnostics only
agent-marketplace doctor --repair       # preview; on a terminal, confirm each action
agent-marketplace doctor --repair --yes # adopt exact matches without prompting
```

- It previews three classes: **exact adoption matches** (entries already
  byte-identical to what the marketplace would write), **evidence-backed orphan
  candidates** (entries a retained backup manifest or journal links to prior
  managed output), and **value conflicts** (an unmanaged entry has the same key
  as a selected resource but a different value). Orphans without evidence and
  unrelated unmanaged entries are never nominated — edit those by hand.
- On a terminal it then prompts per action. **Off a terminal it is
  preview-only** and mutates nothing.
- `--yes` adopts exact matches without prompting. Adoption changes no bytes of
  your file; it only records that an already-identical entry is managed.
- **`--yes` never removes an orphan or replaces a conflicting entry.** Both
  destructive actions always require an interactive per-entry confirmation and
  take a fresh backup first.
- Exit `0` means clean, repaired, or declined. Exit `1` means corrupt ownership
  state, an unparseable aggregate file, or a partial repair failure.
- `--yes` without `--repair` is an error.

`doctor` also detects a locally installed Devin CLI, prints its version, and
warns when it is below the 3000.3 compatibility floor. That warning never blocks
anything: `sync` does not need a Devin binary at all. If a Devin CLI is
installed but no harness list selects it, `doctor` recommends opting in — it
does not do it for you.

---

## Behavior with pre-existing artifacts

`sync` coexists with content you author yourself, but with an important nuance
about *where* you put it. Empirically:

- Hand-authored **skill directories** you place under a harness skills dir (e.g.
  a `.factory/skills/<name>/` directory you created yourself) are **left
  untouched** by `sync`.
- **Loose files are different.** An unmanaged regular file dropped directly into
  a managed component directory — e.g. a `.md` you put in `.factory/commands/`,
  or a stray file inside `.factory/skills/` — **is pruned** by `sync` and shown
  under **ORPHANED FILES** in `status`. Sync preserves directories you own, not
  stray files sitting in directories it manages.
- Otherwise, pruning applies only to artifacts `agent-marketplace` materialized:
  drop a resource from config and re-sync, and only that managed artifact is
  removed (`Removed N`).
- `mcp.json` is **merged**, not overwritten: managed servers are tagged with
  `"_managed_by": "agent-marketplace"`, while servers you added by hand are
  preserved alongside them.
- Devin's shared files (`hooks.v1.json`, `config.json`, `mcp_config.json`) are
  also merged, but through a different mechanism: no marker is written into
  them, and ownership comes from durable state outside the repo. See
  [Devin](#devin) above.

In short: keep hand-authored content in its own skill directory (or as an
unmanaged `mcp.json` entry), not as loose files inside the directories
`agent-marketplace` manages.

---

## Discoverability

There is currently **no central catalog or index** to browse all available
resources across organizations. To see what exists you must either have
registries configured (then `agent-marketplace list`) or point at a specific
registry with `--from`. A central source of truth for discovery is on the
roadmap.

---

## What can be synced

Resources are authored in any supported harness layout and compiled into your
target harness on `sync`.

| Resource kind | Status |
|---------------|--------|
| **Skill** | Supported |
| **Command** | Supported |
| **Subagent** | Supported (dropped on harnesses with no equivalent, e.g. Windsurf, with a diagnostic) |
| **MCP server** | Supported |
| **Hook** | Experimental — materializes, but output fidelity is still being finalized |
| **Collection** | Supported (not a resource itself — a named bundle of the kinds above) |

> **Hooks are experimental and not yet production-ready.** The schema accepts hook
> selectors and they *do* compile and materialize end-to-end (e.g. to
> `.factory/hooks/hooks.json`, reported as synced). However, the generated hook
> output fidelity is not finalized — the compiled JSON can include spurious
> fields — so treat hook syncing with caution before relying on it in production.

When a target harness cannot represent a resource, the compiler emits a
structured diagnostic (visible in `status`) and skips that artifact rather than
silently dropping it.

### Collections

A collection is **not a resource itself** — it is a named bundle that points at
a set of skills, commands, subagents, and/or MCP servers (possibly spanning
multiple registries). Think of it as a curated install list: enabling a
collection enables all of its members, and the members are still the real
resources that get materialized.

Collections are discovered from a registry under
`collections/<name>/COLLECTION.yaml`. You install or remove the bundle as a
unit, and you can still opt out of individual members:

```bash
agent-marketplace list --kind collection             # list available collections
agent-marketplace install platform-onboarding        # install the whole bundle
agent-marketplace remove vm-lifecycle                # exclude one member, keep the rest
agent-marketplace remove platform-onboarding         # remove the whole bundle
```

---

## Local-path registries

Instead of a Git URL, a registry entry can point at a local directory — handy
for developing or testing resources before publishing them. A URL is treated as
a local path when it starts with `/`, `~/`, `./`, or `../` (or is `.`/`..`) and
does **not** end in `.git`. A `ref` on such an entry is simply ignored rather
than making it remote:

```yaml
registries:
  - url: ./local-skills        # resolved relative to the repo root
    source_harness: claude
    skills:
      include: [my-custom-skill]
```

Local registries are read directly from disk (no clone or fetch), are excluded
from `update`, and show `local` as their cache freshness in `status`.

---

## Command reference

```
agent-marketplace list                          Browse resources from configured registries
agent-marketplace list --kind command           Browse one kind: skill, command, subagent, mcp, collection
agent-marketplace list --from <url> --ref <ref>  Browse an ad-hoc registry (nothing written)

agent-marketplace install <name> --from <url> --ref <ref>   Install one resource from a registry
agent-marketplace install --from <url> --ref <ref>          Install ALL resources (bundle mode)
agent-marketplace install <name> --from <url> --path <dir>  Install from a subdirectory
agent-marketplace install <name> --from <url> --kind <kind> Disambiguate by resource kind
agent-marketplace install <collection>                      Install a collection by name
agent-marketplace install --tag <tag>                       Install everything matching a tag
agent-marketplace install --all [--registry <url>]          Install every discovered resource
agent-marketplace install ... --scope {user,repo,local}     Choose which config layer to write

agent-marketplace remove <name> --from <url>     Remove/exclude a resource from a registry entry
agent-marketplace remove --from <url>            Remove an entire ad-hoc registry entry
agent-marketplace remove <collection>            Remove a collection reference
agent-marketplace remove ... --scope {user,repo,local}      Choose which config layer to edit

agent-marketplace sync                           Deterministic materialization from merged config
agent-marketplace sync --check                   Report drift only; exits 1 if out of sync
agent-marketplace update                         Pull the latest commit for each pinned ref
agent-marketplace update --bump                  Discover and bump to newer refs
agent-marketplace status                         Resources, harnesses, targets, refs, source layer, drift
agent-marketplace convert --from <h> --to <h> --out <dir> <path>   One-shot layout conversion
agent-marketplace convert ... --sync                Also prune output files a previous convert --sync wrote
agent-marketplace convert ... --sync --replace-unmanaged   Authorize replacing and pruning output it cannot prove it wrote
agent-marketplace doctor                         Local harness/root diagnostics, Devin CLI detection
agent-marketplace doctor --repair                Preview (and on a terminal, confirm) Devin aggregate ownership recovery
agent-marketplace doctor --repair --yes          Adopt exact matches without prompting; never removes or replaces entries
agent-marketplace tui                            Launch the interactive browser explicitly
```

`--from`/`--to` on `convert` accept `claude`, `factory`, `cursor`, `windsurf`,
`gemini`, and `devin`. `--yes` is only valid together with `--repair`, and
`--replace-unmanaged` is only valid together with `--sync`.

**`convert --sync` prunes only what it can prove it wrote.** Ownership of each
output file is recorded in durable state under
`$XDG_STATE_HOME/agent-marketplace/convert`, one record per output root **and**
`--to` target (never inside the output tree), so a later sync removes exactly the
files an earlier sync of that same target materialized and whose bytes are
unchanged. Two harnesses can therefore be converted into one directory without
either forgetting its own output. Anything else — a handwritten file at the
output root, a hand-authored resource nested beside generated ones, a generated
file the user has since edited, or any file at all when that target's record is
missing or corrupt — is preserved and reported as
`convert.sync.preserved-unmanaged` (the listing is capped; the remainder is
summarized). If a converted resource would land on an existing file with
different, unowned content, the run is refused before anything is written
(`convert.sync.unmanaged-collision`).

**A refusal states the whole scope of the override it suggests.** Before exiting,
the run lists every unowned file `--replace-unmanaged` would replace *and* every
unowned file it would delete, with the totals, so the flag cannot look narrower
than it is. Running with `--replace-unmanaged` prints the same scope
(`convert.sync.replace-unmanaged-authorized`) before it destroys anything, and
`--dry-run` prints it without touching a file.

**A file whose bytes cannot be read behaves differently in each role**, because
ownership is proved by comparing bytes:

- As a **prune candidate** it is unmanaged, so it is preserved by default and
  deleted under `--replace-unmanaged` — deleting needs no read.
- As a **destination** a converted resource must be written to, it stops the
  conversion from comparing or replacing it. The default run is refused with
  `convert.sync.unmanaged-collision` (naming the file, not failing deep inside
  the write), `--replace-unmanaged` removes it and writes the converted file in
  its place, and `--dry-run --replace-unmanaged` reports
  `convert.sync.destination-unreplaceable` because no plan can be computed while
  such a file is there. Anything at a destination that is not a regular file — a
  symlink, a directory — is treated the same way.

A run that cannot prune keeps what it owns. A partial conversion suppresses
pruning entirely, so its record carries forward every earlier entry still on disk
with unchanged bytes; the next clean run can therefore still prune what the
partial one had to leave behind, instead of treating its own earlier output as
unmanaged forever.

A record written by an older version, keyed by output root alone, is still read
for the target it names and is rewritten under its target key by that target's
next successful sync. It never authorizes anything for another target.

**Ad-hoc flag rules:**

- `--ref` and `--path` require `--from`.
- On `install`, `--from` cannot be combined with `--tag` or `--all`; it operates
  only on the given URL.
- Omitting the resource name on `install --from` installs the whole bundle; on
  `remove --from` it removes the entire registry entry.
- `list --from` is browse-only and never writes to any config file.
- GitLab/GitHub web URLs are auto-parsed; explicit `--ref`/`--path` override the
  inferred values.
- Every ad-hoc URL is validated against the `allowed_registries` policy before
  any network fetch or config write.

---

## Status and drift

`status` runs a dry-run materialization, so it reports the exact paths `sync`
would write. It includes:

- **SYNCED RESOURCES** — each selected resource with its `KIND`, `NAME`,
  `HARNESS`, `TARGET`, `SOURCE` layer, originating registry ref, and cache
  freshness.
- **HARNESS TARGETS** — one row per effective harness with its `CAPABILITIES`
  (`compile`, `ingest`, `convert`, `tui`) and both roots (`REPO ROOT` and
  `MACHINE ROOT`). This is where you can see, for example, that `devin` writes
  to `<repo>/.devin` and `~/.config/devin`. A harness name the CLI does not
  recognize is still listed, so a typo is visible rather than silently dropped.
- **LOCALLY EXCLUDED** — resources suppressed by `.agents/config.local.yaml`.
- **LOCAL REGISTRIES** — registry entries defined in `.agents/config.local.yaml`.
- **ORPHANED FILES** — generated files `sync` would prune.
- **NAME COLLISIONS** — same-kind resources offered by multiple registries.
- **ERRORS** — catalog, resolver, or materialization problems.

`sync --check` uses the same dry-run path and exits non-zero on drift, which
makes it ideal for CI and pre-commit hooks:

```bash
agent-marketplace sync --check || (echo "Run 'agent-marketplace sync' to fix" && exit 1)
```

---

## Logging

`agent-marketplace` writes rotating JSON logs to
`~/.agent-marketplace/logs/agent-marketplace.log` out of the box — no
configuration required. The defaults live in code, so the generated config
skeleton intentionally has **no** `logging:` block. Add one to
`~/.agents/config.yaml` only when you want to override a default (level, file
path, rotation), or use the `--debug` flag / `AGENT_MARKETPLACE_*` env vars for
one-off runs. Precedence is **env var > config file > code default**.

See [`docs/logging.md`](./docs/logging.md) for the full strategy, the list of
overridable keys, and the environment variables.

---

## Development

```bash
make build     # build the local binary
make test      # run unit tests
make lint      # run linters (gofmt, go vet, golangci-lint)
make release   # cross-compile versioned artifacts (darwin-arm64/amd64, linux-amd64)
make e2e       # run the end-to-end suite (see below)
```

### E2E tests

The `e2e/` directory contains end-to-end tests that run the built binary in
isolated temp-directory sandboxes. They are **local-only** (CI runners cannot
clone git registries) and optionally run inside Docker for host isolation.

```bash
make e2e                    # runs inside Docker (isolated from host env)
E2E_SKIP_DOCKER=1 make e2e  # run directly on the host
E2E_PARALLEL=1 make e2e     # serial execution for debugging (default 4)
```

There are two kinds of E2E tests:

1. **Declarative (table-driven)** — `TestCase` structs in `e2e/table_test.go`,
   the preferred format for the standard setup/run/assert pattern.
2. **Standalone** — `func TestXxx(t *testing.T)` functions in separate files,
   for tests that need non-standard setup (custom working directory, no git
   repo init, etc.).

Use the fixture builders (`Skill`, `Command`, `Subagent`, `MCPServer`) from
`e2e/harness_test.go` to define registry contents, and reference seeded repos
with `{{REG:name}}` placeholders (resolved to `file://` URLs automatically).
