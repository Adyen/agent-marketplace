# agent-marketplace

`agent-marketplace` is a command-line tool that installs shared AI agent
resources into the AI coding tools you use. A resource is a skill, command,
subagent, hook, or MCP server. The tool reads resources from registries (Git
repositories or local folders) and from hosted Marketplaces, then writes each
one in the native layout of every harness you target: Factory Droid, Claude
Code, Cursor, Windsurf, Gemini CLI, Devin CLI, and Codex.

It is meant for teams and organizations that share agent resources across many
repositories and machines, and for developers who want one command to keep
those resources installed and up to date. It ships as a single Go binary for
macOS and Linux with no runtime dependencies.

## Documentation

The user documentation lives in [`site/docs`](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/index.md).
Good places to start:

- [Install](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/install.md)
- [Quick start](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/quick-start.md)
- [Terminal basics](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/terminal-basics.md),
  if you rarely use a terminal
- [Glossary](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/glossary.md)
  for the terms used on this page
- [CLI reference](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/reference/cli.md)
  and [configuration reference](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/reference/configuration.md)
- [Harnesses and output paths](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/reference/harnesses-and-output-paths.md)

This README is a longer tour of the same behavior for readers who prefer one
page.

## How it works

A [harness](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/glossary.md#harness)
is an AI coding tool such as Claude Code or Codex. Each harness expects its
files in its own folders, for example `.claude/skills/` or `.factory/droids/`.

A [registry](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/glossary.md#registry)
is a Git repository or a local folder that holds resources. In a configuration
file you name the registries you want, pick resources from each, and list the
harnesses you target. When you run `agent-marketplace sync`, the tool fetches
the registries, converts each resource for each harness, and writes the result
into the harness folders. The docs call that last step materializing.

Your configuration is the source of truth. Files that stand alone, such as a
skill folder, can be deleted and recreated with `sync`. Shared configuration
files, such as `.mcp.json`, also hold entries you wrote yourself, so `sync`
edits only its own entries in them and leaves the rest of the file alone.

> Every resource you enable belongs to a specific source, identified by its
> URL, ref, and optional path (or, for a Marketplace, its endpoint and
> registry). There is no global skill or command without a source. This keeps
> installs reproducible and shows where each installed resource came from.

---

## Installation

### From GitHub Releases

Each [release](https://github.com/Adyen/agent-marketplace/releases/latest)
publishes one binary per platform:

```text
agent-marketplace-v4.0.9-darwin-arm64   # macOS, Apple silicon
agent-marketplace-v4.0.9-darwin-amd64   # macOS, Intel
agent-marketplace-v4.0.9-linux-amd64    # Linux, x86-64
agent-marketplace-v4.0.9-linux-arm64    # Linux, ARM64
```

There is no Windows build. Download the file for your platform, make it
executable, and move it to a folder on your `PATH`:

```bash
chmod +x agent-marketplace-v4.0.9-darwin-arm64
mv agent-marketplace-v4.0.9-darwin-arm64 /usr/local/bin/agent-marketplace
```

If `mv` reports "permission denied", or `/usr/local/bin` does not exist, see
the [install guide](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/install.md)
for the `sudo` variant and for installing into `~/.local/bin` without
administrator rights. If macOS refuses to open the file because it was
downloaded from the internet, allow it under System Settings, Privacy &
Security.

### With Go

With Go 1.26.6 or newer:

```bash
go install github.com/Adyen/agent-marketplace/cmd/agent-marketplace@latest
```

This builds the latest commit on the main branch, not a release: Go cannot
select the `v4.x.x` tags for this module path. Go places the binary in
`$(go env GOPATH)/bin`, usually `~/go/bin`. That folder must be on your `PATH`.

### Build from source

Building from source also requires Go 1.26.6 or newer. To build a release,
check out its tag first (for example `git checkout v4.0.9`).

```bash
# Build the local binary (./agent-marketplace)
make build

# Cross-compile versioned release artifacts for
# darwin-arm64, darwin-amd64, linux-amd64, and linux-arm64 into ./dist/
make release
```

Use `make build` for day-to-day local use. `make release` produces the
version-stamped binaries in the same form as the ones published on GitHub
Releases.

### Check the installation

```bash
agent-marketplace --version
```

A release prints its tag, for example `v4.0.9`. `make build` stamps the output
of `git describe --tags --always --dirty`, for example `v4.0.9` on the tag or
`v4.0.9-3-gbaeda4e` on a later commit. A binary from `go install ...@latest`
or a plain `go build` without the version flag prints a Go pseudo-version such
as `v0.0.0-2026...`.

---

## Two ways to use it

| Mode | When to use it | How to launch |
|------|----------------|---------------|
| TUI (interactive) | Browsing, discovering, and toggling resources by hand | run `agent-marketplace` with no arguments in a terminal, or `agent-marketplace tui` |
| CLI (commands) | Scripted, repeatable installs, CI checks, automation | `agent-marketplace <command> ...` |

Both modes read and write the same configuration files. The
[TUI](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/glossary.md#tui)
is a full-screen terminal interface over the commands described below.

The keys you will use most in the TUI are `j`/`k` to move, `/` to search,
`enter` to view details, and `space` to open the scope picker, where `g`, `p`,
and `l` toggle a resource for your user, project, or local configuration. `s`
syncs pending changes, `u` updates Git locks and syncs, `r` reloads from disk,
`H` picks the harnesses to target, `E` opens a configuration file in your
editor, `e` shows errors and warnings, `?` lists every binding, and `q` quits.
The [TUI reference](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/reference/tui.md)
has the full list.

The TUI shows configured and enabled resources before it contacts a
Marketplace. It then loads up to 50 results from each configured Marketplace.
`/` filters the loaded rows as you type, and after 250 ms without input it also
searches the configured Marketplaces. Discovery has no pagination, so narrow
your search or use the `search` command for large result sets.

The TUI caches discovery results per endpoint. Results stay fresh for five
minutes and can remain visible as stale for up to 24 hours while a refresh
fails or an endpoint is down. A failing endpoint does not hide results from
the endpoints that work. Blocked and revoked resources stay visible but cannot
be installed. Before it installs a Marketplace resource, the TUI verifies the
exact revision and shows a verified preview. It asks for confirmation when the
revision has security findings, then writes the exact pin to the selected scope
and reloads.

---

## Quick start

This walkthrough adds a whole registry (a "bundle" install), syncs it, and
checks the result. The [quick start guide](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/quick-start.md)
covers the same ground step by step for first-time users.

`sync` needs to know which harnesses you use. If you have not chosen them yet,
run `agent-marketplace init`. It asks for your harnesses and writes
`~/.agents/config.yaml`. In a script, pass them directly, for example
`agent-marketplace init --harnesses factory,claude`.

```bash
# 1. See what a registry contains. Nothing is written to disk yet.
agent-marketplace list --from git@gitlab.example.com:example-org/infra-skills.git --ref main

# 2. Install ALL resources from that registry (bundle mode).
#    The registry is added to your repo config automatically.
agent-marketplace install --from git@gitlab.example.com:example-org/infra-skills.git --ref main

# 3. Write everything into your harness layout (e.g. .factory/)
agent-marketplace sync

# 4. See exactly what landed on disk, and from which registry/ref
agent-marketplace status
```

The target harnesses come from the project configuration when it lists them,
otherwise from `~/.agents/config.yaml`, otherwise from
`/etc/agent-marketplace/config.yaml`.

To install one resource instead of the whole bundle, name it. A name alone can
be ambiguous, so the registry it comes from is always recorded:

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

You can also paste a GitLab or GitHub web URL. The CLI reads the ref and path
from the `/-/tree/<ref>/<path>` (GitLab) or `/tree/<ref>/<path>` (GitHub) part
of the URL, and when the URL points at a single resource it infers the name
too:

```bash
agent-marketplace install \
  --from https://gitlab.example.com/example-org/infra-skills/-/tree/main/acs-compute-ops/skills/vm-lifecycle
```

After setup, you mostly need these commands:

```bash
agent-marketplace sync          # reconcile writable Git locks and materialized output
agent-marketplace sync --check  # report lock/output drift only; exits 1 if out of sync
agent-marketplace status        # exact locks, installed resources, and drift/errors
agent-marketplace update        # advance Git locks, then synchronize
agent-marketplace list          # browse resources from your configured registries
```

Once the configuration and the generated lockfiles agree, every `sync` writes
the same files from the same Git commits. If you delete generated output, for
example the skill links under `.factory/skills/`, the next `sync` recreates it
from those commits. Use `sync --locked` when the command must refuse to change
configuration or lockfiles, and `sync --check` for a CI check that writes
nothing.

---

## Configuration and precedence layers

Configuration comes in four
[layers](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/glossary.md#configuration-layer).
Each one lives in its own file:

| Layer | File | Scope | Who controls it |
|-------|------|-------|-----------------|
| Organization | `/etc/agent-marketplace/config.yaml` | machine-wide | IT or device management (enforced) |
| User personal | `~/.agents/config.yaml` | machine-wide (all repos) | You, per machine |
| Repo committed | `<repo>/.agents/config.yaml` | this repo (shared) | The team, committed to Git |
| Repo personal | `<repo>/.agents/config.local.yaml` | this repo (just you) | You, gitignored |

When layers disagree about which resources to select, the more specific layer
wins: your repository and personal files override the user and organization
files. The organization layer can still enforce resources, because no layer can
exclude a resource the organization marks `required: true` (see below).

In practice:

- The CLI reads every layer that exists and merges them into one list of
  sources and resources. When one layer selects a repo-scoped resource and
  another excludes it, the more specific layer (repo, then personal) wins.
  `required: true` resources are the exception: no layer can exclude them.
  Repo and checkout-local excludes also do not suppress machine-scoped
  resources.
- Machine-wide layers write into your home harness folders (e.g.
  `~/.factory/`); repository layers write into `<repo>/.factory/`. A resource
  can exist in both places without conflict.
- The CLI records the layer each resource came from, so `status` shows its
  origin, and `--scope` chooses which file `install` and `remove` edit.
- `.agents/config.local.yaml` and `.agents/lock.local.yaml` are personal and
  gitignored. Files that coordinate in-progress transactions live under the XDG
  state directory (`$XDG_STATE_HOME`, default `~/.local/state`), outside
  repositories.
- Any layer can pull in other local files with `include`. See the
  [precedence reference](./docs/PRECEDENCE.md) for include syntax.

A minimal committed repository configuration looks like this. Each `sources:`
entry names a Git, directory, or Marketplace origin, and the `resources:` group
with the same name selects from it. Resource locators have the form
`kind/name`. A project may also declare a top-level `harnesses:` list; without
one, the target harnesses come from `~/.agents/config.yaml` or
`/etc/agent-marketplace/config.yaml`:

```yaml
# .agents/config.yaml  (committed)
apiVersion: agent-marketplace/v4

sources:
  infra:
    git: git@gitlab.example.com:example-org/infra-skills.git
    ref: main
    path: skills              # subdirectory inside the registry (optional)
    source_harness: claude    # layout of the registry content (optional; autodetected)
resources:
  infra:
    include: [skill/vm-lifecycle, skill/k8s-basics, command/deploy-service, subagent/incident-responder]
```

Older v2 and v3 configuration files still load through a compatibility reader.
The first change the CLI makes to one in a run prints a
`config.legacy-deprecated` warning, and `agent-marketplace migrate` converts
them to v4. A v2 file for comparison:

```yaml
# legacy v2 — migrate with `agent-marketplace migrate --scope repo`
apiVersion: agent-marketplace/v2
registries:
  - url: git@gitlab.example.com:example-org/infra-skills.git
    ref: main
    path: skills
    source_harness: claude
    skills:
      include: [vm-lifecycle, k8s-basics]
```

The full precedence and selection rules, including edge cases and error
handling, are in [`docs/PRECEDENCE.md`](./docs/PRECEDENCE.md).

### Monorepos and nested configs

The CLI finds the repository layer by walking up from your current directory to
the nearest ancestor that contains `.agents/config.yaml`. The walk stops at the
boundary of the current Git repository, so a nested repository never picks up
its parent's configuration. It also skips your home directory, whose
`~/.agents/config.yaml` is the user layer. In a monorepo with configurations at
several levels, this means:

- Only one repository configuration applies per run: the closest one above your
  current directory. Configurations at different levels are not merged.
- Generated harness output (`.factory/` and so on) goes next to that
  configuration, not necessarily at the monorepo root.
- From a subdirectory without its own `.agents/config.yaml`, the CLI uses the
  nearest ancestor configuration (for example the monorepo root) and writes
  output there.

For example:

```
mono/.agents/config.yaml                  # selects root-skill
mono/services/team-a/.agents/config.yaml  # selects team-skill
```

| Run from | Config used | Output written to |
|----------|-------------|-------------------|
| `mono/` | `mono/.agents/config.yaml` | `mono/.factory/` |
| `mono/services/team-a/` | `mono/services/team-a/.agents/config.yaml` | `mono/services/team-a/.factory/` |
| `mono/services/team-b/deep/` (no config) | `mono/.agents/config.yaml` (nearest ancestor) | `mono/.factory/` |

Give each independently managed subtree its own `.agents/config.yaml`; that
subtree then acts as its own repository root. The user and organization layers
still apply in every subtree.

A personal `.agents/config.local.yaml` is found the same way, walking up from
your current directory but stopping at the repository root. A subdirectory can
therefore keep its own personal overrides on top of the parent's committed
configuration without becoming a separate root.

---

## Selecting resources: opt-in and opt-out

Every `resources:` group can carry `include` and `exclude` locators and `tags`.
Because each group belongs to a named source, every selected resource is tied
to its origin (`git`, `directory`, or `marketplace`, plus `ref` and `path`).

```yaml
resources:
  infra:
    include: [skill/vm-lifecycle, skill/k8s-basics]   # opt IN to these specific skills
    exclude: [skill/legacy-vm-flow]                   # opt OUT (e.g. pulled in by a tag)
    tags: [k8s]                                       # opt in to everything tagged k8s
```

An exclusion removes a matching selection, and an include in a more specific
layer can select the same resource again. See
[`docs/PRECEDENCE.md`](./docs/PRECEDENCE.md) for the full decision order.

### Machine-wide opt-in with `required` and `all`

A resource group in a machine-wide layer can select resources automatically:

```yaml
# ~/.agents/config.yaml  (or /etc/agent-marketplace/config.yaml for the organization layer)
# `security` and `shared` are named git sources declared in the same file
resources:
  security:
    all: true
    required: true
  shared:
    all: true
```

- `required: true` makes the resources mandatory. No `exclude` in any layer can
  remove them, which is how an organization enforces security standards.
- `all: true` selects every resource the source offers. A more specific layer
  can still exclude individual repo-scoped resources.
- With neither, a resource is selected only when an `include` lists it or a
  `tags` entry matches it.

### Personal config (without touching the repo config)

The gitignored `.agents/config.local.yaml` is your personal layer for one
checkout. You can use it in two ways.

To hide a non-required resource from a named source in this checkout, add an
exclude. The local file needs its own `apiVersion` line and must repeat the
source declaration from the committed file, or the CLI rejects it:

```yaml
# .agents/config.local.yaml  (gitignored, personal)
apiVersion: agent-marketplace/v4
sources:
  infra:
    git: git@gitlab.example.com:example-org/infra-skills.git
    ref: main
    path: skills
resources:
  infra:
    exclude: [skill/deprecated-legacy-skill, mcp/noisy-mcp-server]
```

To add a source for your checkout only, without committing it for the team or
adding it to your user configuration, declare it here:

```yaml
# .agents/config.local.yaml  (gitignored, personal)
apiVersion: agent-marketplace/v4
sources:
  personal:
    git: git@gitlab.example.com:my/personal-skills.git
    ref: main
resources:
  personal:
    all: true
```

`install --scope local` writes the same kind of entry for you:

```bash
agent-marketplace install --from git@gitlab.example.com:my/personal-skills.git --scope local
agent-marketplace sync   # re-materialize with your local registry included
```

In v4, every selector belongs to a named source. In a legacy v2 or v3 local
configuration, a top-level `exclude` still applies to every eligible
repo-scoped registry at once; migrate to v4 to exclude per source.

After you edit any configuration file by hand, run `agent-marketplace sync` to
update the generated files, or `agent-marketplace sync --check` to see whether
anything is out of date without writing.

---

## Materialization mode

By default, `sync` installs skills as
[symlinks](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/glossary.md#symlink)
into the local cache. To write regular files and folders instead, for example
on a filesystem or tool that does not follow links, set
`materialization.mode: file`:

```yaml
# ~/.agents/config.yaml — default for user and project output
materialization:
  mode: file
```

A committed `.agents/config.yaml` or personal `.agents/config.local.yaml` can
set the same block. There it changes project-scoped output only, never
machine-scoped resources selected in the user configuration. File mode records
what it wrote in `.agents/materialization-manifest.yaml`. The manifest holds
only target-relative paths. The CLI does not add it to `.gitignore` and never
stages or commits it. For user-scope output, file mode keeps its manifest under
`~/.agent-marketplace/`.

Every harness supports both modes. You can set a mode per harness to override
the default for that harness:

```yaml
materialization:
  mode: file
  targets:
    devin: symlink
```

Factory commands are always copied as regular files, in either mode, because
the Droid CLI does not follow symlinked command files. Shared configuration
files that hold hooks and MCP servers are always real files, updated entry by
entry, and the mode does not change how their ownership is tracked.

---

## Codex

Codex is not in any default harness list. To use it, press `H` in the TUI and
tick Codex, or add it to a `harnesses:` list:

```yaml
# ~/.agents/config.yaml
apiVersion: agent-marketplace/v4
harnesses: [factory, codex]
```

Codex uses two folders per scope. If you set `CODEX_HOME`, it must be an
absolute path, and it moves only the machine `.codex` folder; user skills stay
under `~/.agents`.

| Scope | Skills and converted commands | Subagents, hooks, and MCP servers |
|---|---|---|
| Repository | `<repo>/.agents/skills/` | `<repo>/.codex/` |
| Machine | `~/.agents/skills/` | `$CODEX_HOME/` or `~/.codex/` |

| Resource kind | Codex output |
|---|---|
| Skill | `.agents/skills/<name>/` containing `SKILL.md` |
| Command | `.agents/skills/<name>/` containing a `SKILL.md` marked `x-agent-marketplace-kind: command` |
| Subagent | `.codex/agents/<name>.toml` |
| Hook | `.codex/hooks.json` |
| MCP server | `[mcp_servers.<name>]` in `.codex/config.toml` |

Codex works in both directions: `convert --from codex`, `convert --to codex`,
and `source_harness: codex` are all valid. Autodetection requires a `.codex`
folder, because `.agents/skills` on its own is a layout several tools share. In
the default symlink mode, each skill or command folder is a link into the
cache; file mode writes a regular folder instead. `convert --to codex` refuses
a destination that already contains `.codex/hooks.json` or
`.codex/config.toml`. Choose a new or empty destination, so conversion never
takes over or overwrites existing shared configuration.

You may also edit `hooks.json` and `config.toml` by hand, so `sync` changes only
the entries its ownership records show it wrote. Those records live under
`$XDG_STATE_HOME/agent-marketplace` (default `~/.local/state/agent-marketplace`).
`sync` keeps comments and unrelated entries, and it saves a byte-for-byte
backup of the file before each change. If the ownership records are missing or
corrupt, `sync` treats every existing entry as yours: it refuses to overwrite a
colliding entry and removes nothing. A malformed file blocks only that file.

Codex may ask you to review project trust after a repository's hooks or MCP
configuration change. A `sync` that changes them reports
`materialize.codex.trust-review`, and `sync --check` reports
`materialize.codex.trust-review-pending` when a change is waiting. Agent
Marketplace never edits Codex trust settings and never adds Codex output paths
to `.gitignore`.

Codex rules and `AGENTS.md` generation are out of scope, and the compiler never
writes the deprecated `.codex/prompts/*.md` files. Existing prompts can be read
as commands, with an `ingest.codex.prompt-deprecated` warning, so you can move
them to command skills. Agent Marketplace never installs, requires, or runs a
Codex executable; sync, check, ingestion, and conversion work from the file
layout alone.

---

## Devin

Devin CLI is not in any default harness list either. Select it like any other
harness: press `H` in the TUI and tick Devin CLI, or add `devin` to a
`harnesses:` list:

```yaml
# ~/.agents/config.yaml
apiVersion: agent-marketplace/v4
harnesses: [factory, devin]
```

Devin writes to `<repo>/.devin` for the repository and `~/.config/devin` for
the machine, and supports all five resource kinds:

| Resource kind | Devin output |
|---------------|--------------|
| Skill | `skills/<name>/SKILL.md` (directory symlink in the default mode) |
| Command | `skills/<name>/SKILL.md`, marked `x-agent-marketplace-kind: command` |
| Subagent | `agents/<name>.md` (file symlink in the default mode) |
| Hook | repo: `hooks.v1.json`; machine: the `hooks` member of `config.json` |
| MCP server | `mcp_config.json` (same relative path in both scopes) |

Rules are out of scope for this harness. A rule produces a
`rule.devin.unsupported` warning instead of a file, and the run continues.

Devin also works as a conversion source and target, so `convert --from devin`,
`convert --to devin`, and `source_harness: devin` on a source all work.
Autodetection prefers a `.devin` workspace folder over any other layout at the
same root. A user-style root, with `skills/`, `agents/`, `config.json`, and
`mcp_config.json` at its top level, is never autodetected; pass `--from devin`
for it.

### Shared Devin files are never overwritten

You may also edit `hooks.v1.json`, `config.json`, and `mcp_config.json` by hand,
so `sync` updates them entry by entry instead of rewriting them:

- It does not touch hooks and MCP servers you wrote. It keeps their comments,
  key order, and relative order, and your hooks keep running before generated
  ones.
- It writes no ownership marker into a Devin file (no `"_managed_by"` key).
  Ownership records live under `$XDG_STATE_HOME/agent-marketplace` (default
  `~/.local/state/agent-marketplace`), outside the repository and outside the
  cache. Deleting the cache has no effect on ownership.
- If the ownership records are missing or corrupt, `sync` treats every existing
  entry as yours. It keeps everything, refuses to overwrite colliding entries,
  and removes nothing. At worst you are left with a stale entry to delete
  yourself.
- Before it changes a Devin file, `sync` saves a byte-for-byte backup, even
  when configured backups are turned off. It keeps the five newest backups per
  file.
- Each file is updated on its own. A malformed `mcp_config.json` blocks only
  that file and stays exactly as it was; skills, subagents, and a valid hooks
  file are still written.

### `doctor --repair`

`agent-marketplace doctor --repair` repairs the ownership records for the
shared Devin and Codex files (`hooks.v1.json`, `config.json#hooks`,
`mcp_config.json`, `.codex/hooks.json`, and `.codex/config.toml`). It never
edits a `harnesses:` list.

```bash
agent-marketplace doctor                # detection and diagnostics only
agent-marketplace doctor --repair       # preview; on a terminal, confirm each action
agent-marketplace doctor --repair --yes # adopt exact matches without prompting
```

- It previews three kinds of action. It can adopt an entry that already matches
  exactly what Agent Marketplace would write. It can remove an orphan, an entry
  that a kept backup manifest or journal links to earlier managed output. It
  can replace a conflicting entry, one you wrote with the same key as a
  selected resource but a different value. It never suggests removing an orphan
  without that evidence, or touching unrelated entries of yours; edit those by
  hand.
- On a terminal it asks about each action. Without a terminal it only previews
  and changes nothing.
- `--yes` adopts exact matches without asking. Adopting changes no bytes in the
  file; it only records that the identical entry is managed.
- `--yes` never removes an orphan or replaces a conflicting entry. Both always
  need a confirmation per entry on a terminal, and both save a fresh backup
  first.
- Exit code `0` means clean, repaired, or declined. Exit code `1` means corrupt
  ownership records, a file that cannot be parsed, or a repair that partly
  failed.
- `--yes` without `--repair` is an error.

`doctor` also detects an installed Devin CLI, prints its version, and warns
when it is older than 3000.3. The warning never blocks anything, because `sync`
does not need a Devin binary. If a Devin CLI is installed but no harness list
includes `devin`, `doctor` suggests adding it but does not change your
configuration.

---

## Behavior with pre-existing artifacts

`sync` shares the harness folders with content you create yourself:

- It leaves your own folders and regular files in harness folders alone, for
  example a `.factory/skills/<name>/` folder or a droid file in
  `.factory/droids/` that you wrote.
- It replaces or removes a symlink only when the link points into the Agent
  Marketplace cache. It keeps other symlinks and reports each one with a
  `materialize.collision.user-owned` message.
- Factory command files are copies that carry a `_managed_by` marker in their
  frontmatter. `sync` removes only command files with that marker.
- In file mode, `sync` removes only files listed in its manifest, and keeps any
  of those files you have edited since it wrote them, with a
  `materialize.collision.user-owned` warning.
- When you drop a resource from configuration and sync again, `sync` removes
  only that resource's output and counts it as removed. `status` lists pending
  removals under ORPHANED FILES.

Shared configuration files hold entries from several sources, so `sync`
updates them entry by entry and keeps entries you wrote:

- Factory, Cursor, and Windsurf MCP files, the Claude MCP files (`<repo>/.mcp.json`
  and `mcpServers` in `~/.claude.json`), and hook files on these harnesses and
  Gemini mark each generated entry with `"_managed_by": "agent-marketplace"`.
- Gemini MCP servers in `.gemini/settings.json` carry no marker. Their
  ownership comes from durable records instead, because Gemini's settings
  format rejects the extra field. An old marked Gemini entry without such a
  record is kept and reported, never adopted or stripped automatically.
- Devin files (`hooks.v1.json`, `config.json`, `mcp_config.json`) and Codex
  files (`.codex/hooks.json`, `.codex/config.toml`) also use durable ownership
  records and no marker. See [Devin](#devin) and [Codex](#codex) above.
- A generated entry never overwrites or deletes an entry you wrote. For Claude
  and Gemini MCP servers and for the Devin and Codex files, `sync` leaves the
  whole file unchanged when one of your entries has the same name as a
  generated one, or when the file cannot be parsed.

Keep hand-written content in its own folder, file, or entry, and it survives
every `sync`.

---

## Hosted Marketplace

Besides Git and directory registries, `agent-marketplace` can install from a
hosted [Marketplace](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/glossary.md#marketplace),
a server that indexes registries so you can search them in one place and
install verified, exact revisions.

```bash
agent-marketplace install https://marketplace.example.com            # register an endpoint (user scope by default)
agent-marketplace search review --kind skill                          # search every configured endpoint
agent-marketplace install https://marketplace.example.com/r/shared-agents/skill/review \
  --scope repo                                                        # install one exact, verified revision
```

Registering an endpoint installs nothing, and registering one that already
exists in the same or a broader scope changes nothing. `search --output json`
returns results together with the errors from endpoints that failed. The TUI
shows Marketplace results next to your configured resources (see
[Two ways to use it](#two-ways-to-use-it)).

In configuration, a Marketplace is a source like any other. `install` adds a
plain locator to the source's resource group and records the exact verified
revision in the lockfile of the same layer:

```yaml
sources:
  company:
    marketplace: https://marketplace.example.com
resources:
  company:
    include:
      - shared-agents/skill/review
```

To pin a revision in the configuration itself, give the locator a `revision:`
option, for example `- shared-agents/skill/review: {revision: sha256:<64 hex characters>}`.

`agent-marketplace serve` runs the Marketplace server itself. See
[How to install a resource from a hosted Marketplace](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/how-to/install-from-a-marketplace.md)
and the [operator guide](./docs/marketplace-operator.md).

---

## What can be synced

Resources can be written in any supported harness layout; `sync` converts them
for each target harness.

| Resource kind | Status |
|---------------|--------|
| Skill | Supported |
| Command | Supported |
| Subagent | Supported (dropped with a diagnostic on harnesses with no equivalent, such as Windsurf) |
| MCP server | Supported |
| Hook | Supported |
| Collection | Supported (a named bundle of the kinds above, not a resource of its own) |

Hooks with the same name from independent registries are all installed. For
every other kind, one registry wins a name collision (see
[`docs/PRECEDENCE.md`](./docs/PRECEDENCE.md)). A hook event that the target harness
cannot represent is skipped with a diagnostic such as `hook.event-unmappable`.

When a target harness cannot represent a resource, the compiler reports a
structured diagnostic (visible in `status`) and skips that file instead of
dropping it without a word.

### Collections

A [collection](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/glossary.md#collection)
is a named bundle of skills, commands, subagents, hooks, and MCP servers,
possibly from several registries. `sync` writes nothing for the collection
itself. Enabling a collection enables its members, and the members are the
resources that get installed.

A registry defines collections in `collections/<name>/COLLECTION.yaml`. You
install or remove the bundle as a unit and can still opt out of single
members:

```bash
agent-marketplace list --kind collection             # list available collections
agent-marketplace install platform-onboarding        # install the whole bundle
agent-marketplace remove vm-lifecycle                # exclude one member, keep the rest
agent-marketplace remove platform-onboarding         # remove the whole bundle
```

## Local-path registries

A source can point at a local folder instead of a Git URL, which helps while you
develop or test resources before publishing them. In v4, use a `directory:`
source:

```yaml
sources:
  local:
    directory: ./local-skills   # resolved against the repository root for repo and local configs
    source_harness: claude
resources:
  local:
    include: [skill/my-custom-skill]
```

In a repository or personal-local configuration, a relative `directory:` is
resolved against the repository root (the folder that contains `.agents/`); in
other files it is resolved against the folder of the declaring file. A relative
path must stay inside that folder. Absolute paths are allowed.

For `--from` arguments and legacy `url:` values, a value counts as a local path
when it is `.`, starts with `./`, `/`, or `~/`, and does not end in `.git`.
Legacy entries reject a URL with a `..` component. A hostless `file://` URL is
local only when its decoded path does not end in `.git`;
`file:///path/to/registry.git` is a remote Git registry, requires a `ref`, and
appears in lockfiles. A `ref` on a local folder is ignored.

The CLI reads local registries straight from disk without cloning or fetching,
leaves them out of `update` and Git lockfiles, and shows `local` as their cache
state in `status`. In legacy v2 and v3 configurations, `path` can name the
registry location when `url` is omitted; when `url` is present, `path` means a
subdirectory inside that registry.

---

## Git lockfiles

A [lockfile](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/glossary.md#lockfile)
records the exact commit that each remote Git registry resolved to. Each
configuration layer owns its own generated lock:

| Configuration layer | Generated lock |
|---|---|
| `/etc/agent-marketplace/config.yaml` | `/etc/agent-marketplace/lock.yaml` |
| `~/.agents/config.yaml` | `~/.agents/lock.yaml` |
| `.agents/config.yaml` | `.agents/lock.yaml` |
| `.agents/config.local.yaml` | `.agents/lock.local.yaml` |

Commit `.agents/lock.yaml` together with the repository configuration. The
personal-local lock is gitignored. An organization may deploy its lock next to
its configuration, but does not have to. Without one, consumer commands leave
the organization's requirements unlocked: they may read or refresh the cache,
and they never create the lock. When the organization lock exists, it is exact,
and anything that does not match it is refused.

A normal `sync` keeps compatible resolutions, updates writable locks that are
out of date, and installs from the exact, verified commits. The only exception
is unlocked organization requirements. Use `sync --locked` when changes to
configuration and locks must be refused, or `sync --check` to check lock and
output drift without writing. Use `lock --upgrade` or `update` when you want to
move Git resolutions forward.

While it updates locks, the CLI holds a lease on the shared cache from the
moment it fetches a commit until it has written the configuration and lock, so
garbage collection cannot delete a new snapshot before the lock refers to it.
`install`, `remove`, `update`, and the TUI write configuration and lock
together in one transaction.

Lock v2 (`agent-marketplace-lock/v2`) is the only format the CLI writes, for
every writable layer, including layers that contain only Git sources. A
writable v1 lock is upgraded to v2 the next time it is updated. A read-only
organization v1 lock is still accepted, with a `lock.version-deprecated`
warning. Lock v2 also records the exact Marketplace endpoint, registry,
resource, and selection used by v4 layers. Plain local-directory registries
are read live and never locked.

v4.0.8 rejects Git-only v2 locks with `lock.invalid-document`. Every client
that reads a committed v2 lock, including CI, needs v4.0.9 or newer.

---

## Command reference

```
agent-marketplace list                          Browse resources from configured registries
agent-marketplace list --kind command           Browse one kind: skill, command, subagent, hook, mcp, collection
agent-marketplace list --from <url> --ref <ref>  Browse an ad-hoc registry (nothing written)
agent-marketplace search [query]                Search configured Marketplace endpoints
agent-marketplace search [query] --marketplace <alias|url|all> --output json

agent-marketplace init [--harnesses <list>]     Create ~/.agents/config.yaml with your target harnesses
agent-marketplace install <marketplace-url>     Register a Marketplace endpoint (user scope by default)
agent-marketplace install <marketplace-url>/r/<source>/<kind>/<name>   Install a Marketplace resource
agent-marketplace install <alias>:<source>/<kind>/<name>    Same, using a registered endpoint's name
agent-marketplace install <name> --from <url> --ref <ref>   Install one resource from a registry
agent-marketplace install --from <url> --ref <ref>          Install ALL resources (bundle mode)
agent-marketplace install <name> --from <url> --path <dir>  Install from a subdirectory
agent-marketplace install <name> --from <url> --kind <kind> Disambiguate by resource kind
agent-marketplace install <collection>                      Install a collection by name
agent-marketplace install --tag <tag>                       Install everything matching a tag
agent-marketplace install --all [--kind <kind>] [--registry <url>]   Install every discovered resource
agent-marketplace install ... --scope {user,repo,local}     Choose which config layer to write

agent-marketplace remove <name> [--kind <kind>]              Remove or exclude a resource
agent-marketplace remove <name> --from <url> [--path <dir>]  Remove/exclude a resource from a registry entry
agent-marketplace remove --from <url> [--path <dir>]         Remove an entire ad-hoc registry entry
agent-marketplace remove <collection>            Remove a collection reference
agent-marketplace remove ... --scope {user,repo,local}      Choose which config layer to edit

agent-marketplace lock                           Create or reconcile user, repo, and local Git locks
agent-marketplace lock --check                   Report lock changes without writing
agent-marketplace lock --scope organization      Admin-only organization lock generation/verification
agent-marketplace lock --upgrade                 Advance Git resolutions to current refs
agent-marketplace sync                           Materialize from exact Git snapshots; reconcile stale writable locks
agent-marketplace sync --locked                  Require fresh locks; never update config or locks
agent-marketplace sync --check                   Report lock or output drift only; exits 1 if out of sync
agent-marketplace sync --offline                 Use only trusted local registry and Marketplace caches
agent-marketplace sync --strict                  Treat unresolved-selection warnings as errors
agent-marketplace update                         Advance Git locks and synchronize resources
agent-marketplace update --bump                  Rewrite eligible legacy registry refs, advance locks, and synchronize
agent-marketplace update --scope {user,repo,local,all}      Limit which layers are updated
agent-marketplace status                         Exact locks, resources, harnesses, refs, source layer, drift
agent-marketplace migrate [--scope <scope>] [--dry-run]     Convert legacy v2/v3 configs to v4
agent-marketplace convert --from <h> --to <h> [--out <dir>] <path>   One-shot layout conversion
agent-marketplace doctor                         Local harness/root diagnostics, Devin CLI detection
agent-marketplace doctor --repair                Preview (and on a terminal, confirm) Devin/Codex ownership recovery
agent-marketplace doctor --repair --yes          Adopt exact matches without prompting; never removes or replaces entries
agent-marketplace backup {create,list,restore}   Manage backups of machine-scope harness folders
agent-marketplace gc [--dry-run]                 Remove orphaned registry cache directories
agent-marketplace deinit [--scope <scope>] [--purge-config] [--dry-run]   Remove managed output and cache
agent-marketplace telemetry {status,enable,disable,preview,purge,reset-id}
agent-marketplace serve [--role worker|api|all]  Run the Marketplace server
agent-marketplace tui                            Launch the interactive browser explicitly
agent-marketplace completion <shell>             Generate a shell completion script
```

`--from` and `--to` on `convert` accept `claude`, `factory`, `cursor`,
`windsurf`, `gemini`, `devin`, and `codex`; without `--out`, output goes to
`<path>-<to>`. `--yes` on `doctor` is valid only together with `--repair`. Run
any command with `--help` for its full flag list, or see the
[CLI reference](https://github.com/Adyen/agent-marketplace/blob/main/site/docs/reference/cli.md).

Rules for ad-hoc `--from` operations:

- `--ref` and `--path` require `--from`.
- On `install`, `--from` cannot be combined with `--tag` or `--all`; it works
  only on the given URL.
- Without a resource name, `install --from` installs the whole bundle and
  `remove --from` removes the entire registry entry.
- `list --from` only browses and never writes a configuration file.
- The CLI parses GitLab and GitHub web URLs; an explicit `--ref` or `--path`
  overrides the value read from the URL.
- For `remove --from`, the URL, path, and ref identify the configured entry.
  You can omit `--path` or `--ref` only when the URL matches one location;
  when entries differ by path or ref, pass the matching flag. A named removal
  with `--path` also covers resources discovered below that path, and any
  equivalent spelling of the URL and path is accepted.
- The CLI checks every ad-hoc URL against the `allowed_registries` policy
  before it fetches anything or writes configuration.

---

## Status and drift

`status` runs `sync` without writing, so it reports the exact paths `sync`
would write. Depending on your setup, it prints these sections:

- TARGET HARNESS SET: the effective harnesses and the layer they came from.
- EXACT REGISTRY LOCKS: each locked Git source with its configuration layer,
  registry, declared ref, exact commit, and whether the cached snapshot is
  available.
- SYNCED RESOURCES: each selected resource with its `KIND`, `NAME`, `HARNESS`,
  `SOURCE` layer, `VIA` (the collection that pulled it in, if any), `TARGET`,
  `REGISTRY`, `REF`, and `CACHE` state.
- MARKETPLACE PINS: each pinned Marketplace resource with its endpoint,
  registry, revision, layer, and cache state.
- HARNESS TARGETS: one row per effective harness with its `CAPABILITIES`
  (`compile`, `ingest`, `convert`, `tui`), `REPO ROOT`, and `MACHINE ROOT`.
  Here you can see Devin's single root and Codex's split between `.agents` and
  `.codex`. A harness name the CLI does not recognize is still listed, so a typo
  stays visible.
- LOCALLY EXCLUDED: resources hidden by a legacy v2 or v3
  `.agents/config.local.yaml`. Exclusions in a v4 local file are not listed.
- LOCAL REGISTRIES: sources defined in `.agents/config.local.yaml`.
- ORPHANED FILES: generated files the next `sync` would remove.
- NAME COLLISIONS: same-kind resources offered by more than one registry.
- UNMET REQUIREMENTS: selected resources that require another resource that is
  not selected, with a suggested fix.
- WARNINGS, then DRIFT (`In sync`, or how many files would be written or
  removed).
- ERRORS: catalog, resolver, or materialization problems.

`sync --check` takes the same path without writing and exits with a non-zero
code on drift, so it fits CI jobs and pre-commit hooks:

```bash
agent-marketplace sync --check || (echo "Run 'agent-marketplace sync' to fix" && exit 1)
```

---

## Logging

`agent-marketplace` writes rotating JSON logs to
`~/.agent-marketplace/logs/agent-marketplace.log` without any configuration.
The defaults live in the code, so the generated configuration file has no
`logging:` block. Add one to `~/.agents/config.yaml` only to override a default
(level, file path, rotation), or use the `--debug` flag or
`AGENT_MARKETPLACE_*` environment variables for a single run. An environment
variable overrides the configuration file, which overrides the built-in
default.

See [`docs/logging.md`](./docs/logging.md) for the full strategy, the keys you
can override, and the environment variables.

---

## Development

Read [`CODING_STANDARDS.md`](./CODING_STANDARDS.md) before changing code, and
[`AGENTS.md`](./AGENTS.md) for the design notes and validation workflow.

```bash
make build          # build the local binary
make test           # run root-module tests (TEST_PACKAGES / TEST_FLAGS narrow them)
make test-fast      # the same tests in -short mode
make lint           # tidy, gofmt, go vet, golangci-lint (rewrites files)
make ci-lint        # the same checks without rewriting files, as CI runs them
make validate-fast  # short-mode tests, ci-lint, and build
make validate       # tests, ci-lint, and build
make validate-full  # fresh full tests, ci-lint, build, E2E, compatibility checks, TUI QA
make validate-e2e   # E2E and configuration-compatibility scenarios in one run
make e2e            # run the end-to-end suite (see below)
make tui-qa         # real-terminal TUI journeys
make release        # cross-compile darwin-arm64/amd64 and linux-amd64/arm64 into ./dist/
```

While you iterate, run the affected package first, for example
`make test-fast TEST_PACKAGES=./internal/resolver TEST_FLAGS="-run TestResolve -count=1"`,
and run the full gates once before you hand off. `make lint` rewrites files, so
do not run it at the same time as other checks.

### E2E tests

The `e2e/` directory holds end-to-end tests that run the built binary in
isolated temporary sandboxes. Registry fixtures are local bare Git repositories
reached through `file://` URLs, so CI runs the whole suite directly on Ubuntu
(`make marketplace-gate`). Local runs use Docker by default for extra isolation
from your machine.

```bash
make e2e                    # run locally inside Docker
E2E_SKIP_DOCKER=1 make e2e  # run directly on the host, as CI does
E2E_PARALLEL=1 make e2e     # serial execution for debugging (default 4)
```

There are two kinds of E2E test:

1. Declarative (table-driven) tests are `TestCase` structs in
   `e2e/table_test.go`. Use this format for the usual setup, run, and assert
   pattern.
2. Standalone tests are `func TestXxx(t *testing.T)` functions in separate
   files, for tests that need unusual setup, such as a custom working directory
   or no Git repository.

Define registry contents with the fixture builders (`Skill`, `Command`,
`Subagent`, `MCPServer`) from `e2e/harness_test.go`, and refer to seeded
repositories with `{{REG:name}}` placeholders, which resolve to `file://` URLs.
