# Repository configuration reference

This page lists every field of an Agent Marketplace configuration file and the rules the CLI checks when it reads one. Use it when you write or review `.agents/config.yaml` by hand, or when a command reports a `v4.*` [diagnostic](../glossary.md#diagnostic) and you need to know which rule you broke.

A configuration file is a [YAML](../glossary.md#yaml) file. The committed `.agents/config.yaml` says which sources a repository uses, which resources it takes from each source, and which [harnesses](../glossary.md#harness) receive them. The same schema is used by the user, organization, and personal-local files described in the [configuration layers reference](configuration-layers.md).

For task-oriented guidance, see [How to adopt agent-marketplace in a repository](../how-to/adopt-agent-marketplace-in-a-repository.md) and [How to keep resources reproducible](../how-to/keep-resources-reproducible.md). The [sources and resources explanation](../explanation/sources-and-resources.md) describes what each map is for and how the two work together.

Configuration holds your selection policy. Generated lockfiles record the exact Git commits and Marketplace revisions that the policy resolved to; see the [lockfiles reference](lockfiles.md). The CLI adapts v4 sources into its internal requirement model on every run. You can edit `sources` and `resources` by hand, or let `install`, `remove`, `update`, and the TUI edit them for you. Those commands edit only the file they target and never rewrite an included fragment; a request that lives in a fragment is refused with `config.edit.include-owned`.

## Schema versions

`apiVersion: agent-marketplace/v4` is the current schema, and every page in this reference describes v4. New configuration files created by `install`, `remove`, `init`, and the TUI use v4.

The CLI still reads `agent-marketplace/v2` files and the `agent-marketplace/v3` alias through a compatibility reader. Both are deprecated: each write to such a file prints a `config.legacy-deprecated` warning with the `migrate` command for that scope. A file with no `apiVersion` at all is not read as v4. See [How to migrate configuration](../how-to/migrate-configuration.md).

## File location and scope

To find the repository configuration, Agent Marketplace starts in the current directory and walks up toward the filesystem root. It uses the first directory that contains `.agents/config.yaml`, with two limits:

- It skips your home directory, because `~/.agents/config.yaml` is the user file.
- It does not leave the current Git repository. A nested repository never picks up a parent repository's configuration.

If no directory qualifies, the Git repository root is used, and outside Git the current directory is used. Configuration files found at different levels are never merged.

The personal-local file is searched the same way, from the current directory up to that repository root. This lets a subdirectory of a monorepo keep its own `.agents/config.local.yaml` on top of the committed file at the root.

Repository-scoped output is written under the directory that holds `.agents/`.

The four configuration layers are:

| Layer | Configuration | Generated lock | Purpose |
|---|---|---|---|
| Organization | `/etc/agent-marketplace/config.yaml` | `/etc/agent-marketplace/lock.yaml` | Managed machine policy and required resources |
| User | `~/.agents/config.yaml` | `~/.agents/lock.yaml` | Personal machine-wide targets and resources |
| Repository | `<repo>/.agents/config.yaml` | `<repo>/.agents/lock.yaml` | Shared project targets and resources |
| Repository personal | `<repo>/.agents/config.local.yaml` | `<repo>/.agents/lock.local.yaml` | Choices for one checkout, kept out of Git |

Commit the repository lock with its configuration. Keep the personal-local files out of Git: when `<repo>/.agents/config.local.yaml` exists, the CLI appends `.agents/config.local.yaml` and `.agents/lock.local.yaml` to the repository's `.gitignore` if they are missing. Consumer commands never create an absent organization lock. Directory sources are read live and never appear in a lock.

## Schema

A v4 root file must declare exactly `agent-marketplace/v4`:

```yaml
apiVersion: agent-marketplace/v4
sources:
  shared:
    git: https://github.com/example-org/agent-resources.git
    ref: v1.4.0
    path: resources
resources:
  shared:
    include:
      - skill/code-review
      - command/release
```

The `sources` map holds named source declarations. The `resources` map holds one resource group per source name, and each group says what to take from that source. A source name is a label inside one configuration file and its includes. It does not identify what gets fetched (the URL, ref, and path do that), and it does not choose where resources install (the layer does that).

A source declares exactly one kind:

- `git` is a Git repository and requires `ref`.
- `directory` is a folder on disk, read live on every run. In the repository `.agents/config.yaml` and `.agents/config.local.yaml` files, relative paths resolve from the repository root. In user and organization files, in included fragments, and in any other file, they resolve from the directory of the file that declares them.
- `marketplace` is a hosted Marketplace endpoint.

The kind is always explicit. A path is never treated as a directory source because it lacks a `.git` suffix.

The parser rejects:

| Problem | Diagnostic |
|---|---|
| Unknown top-level field | `v4.unknown-field` |
| Legacy installation key such as `registries`, `skills`, or `marketplaces` | `v4.legacy-key` |
| YAML alias | `v4.alias-forbidden` |
| YAML merge key (`<<`) | `v4.merge-key-forbidden` |
| Duplicate mapping key | `v4.duplicate-key` |
| More than one YAML document in the file | `v4.multiple-documents` |
| Root file without `apiVersion` | `v4.api-version-required` |
| `apiVersion` other than `agent-marketplace/v4` | `v4.api-version` |

Included fragments may omit `apiVersion` and inherit v4 from the file that includes them. A fragment that declares a different version is rejected.

## Configuration layers

Layers merge from lowest to highest precedence: organization, user, repository, repository personal. Top-level target harnesses are chosen project first: a non-empty repository `harnesses` list wins, then a non-empty user list, then the organization list. A resource group's `harnesses` list narrows that set and cannot add a harness that is not already selected.

Each layer allows only some fields. For example, `telemetry`, `backup`, and `logging` belong to machine-level files, and the personal-local file cannot declare `harnesses` or allowlists. The [configuration layers reference](configuration-layers.md) has the full table.

## `include`

`include` pulls local YAML files into the layer that declares it. It accepts one path or an ordered list of paths:

```yaml
apiVersion: agent-marketplace/v4
include: profiles/company.yaml
```

Use a list when including more than one fragment:

```yaml
include:
  - profiles/company.yaml
  - profiles/payments-team.yaml
```

Relative paths resolve from the directory of the file that declares them. Absolute paths are accepted. Includes can nest. A fragment shared by two branches of the include tree is loaded once for each branch; the loader reports a cycle (`v4.include-cycle`) only when a file includes one of its own ancestors. Glob patterns and remote URLs are not supported.

An included fragment has lower precedence than the file that includes it. In a list, the first fragment merges first, each later fragment merges over the earlier ones, and the including file merges last. Fields keep their own merge rules:

- A source name declared in more than one file must have identical values everywhere. Different values fail with `v4.source-conflict`.
- Resource groups with the same name are combined. Their `include` and `exclude` lists are concatenated in merge order. `tags`, `all`, `registries`, `required`, and `harnesses` take the value from the last file that sets them.
- `allowed_registries` and `allowed_marketplaces` take the first declaration in merge order. A fragment that sets an allowlist cannot be relaxed by a later fragment or by the including file.
- `harnesses` takes the value from the last file that sets it. `backup`, `logging`, `update_check`, `telemetry`, and `materialization` merge field by field, with later files winning.

The loader rejects the whole layer when an include is missing, unreadable, malformed, not allowed in that layer, or part of a cycle. Fragments must use only the fields their layer allows. Organization fragments have extra ownership and permission requirements; see the [configuration layers reference](configuration-layers.md#include).

## Root fields

Unknown top-level fields are rejected.

| Field | Type | Required | Description |
|---|---|---:|---|
| `include` | path or list of paths | no | Local YAML fragments merged into this layer. Relative paths resolve from the declaring file. |
| `apiVersion` | string | yes for root files | Must equal `agent-marketplace/v4`. |
| `harnesses` | list of strings | no | Target harnesses. When omitted or empty in the repository file, targets come from the user file, then the organization file. |
| `sources` | mapping | no | Named Git, directory, or Marketplace source declarations. |
| `resources` | mapping | no | Resource groups, each keyed by a source name. |
| `allowed_marketplaces` | list of regular expressions | no | Restricts hosted Marketplace endpoint URLs. |
| `allowed_registries` | list of regular expressions | no | Restricts remote Git origins. |
| `backup` | mapping | no | Pre-sync backups of machine-scope harness folders: `enabled` (default `true`), `max_days` (default `14`), `max_per_day` (default `10`). Both limits must be positive. Organization and user files only. |
| `logging` | mapping | no | Log file settings: `enabled`, `level`, `file`, `max_size_mb`, `max_backups`, `max_age_days`, `compress`. See the [logging reference](logging.md). |
| `update_check` | mapping | no | Release upgrade notice: `enabled` (default `true`). Organization and user files only; the user value wins. |
| `telemetry` | mapping | no | Machine telemetry policy: `level`, `endpoint`, `community_reporting`, `retention_policy`, `ca_file`, `allow_insecure`, `headers`. See the [telemetry reference](telemetry.md). Organization and user files only. |
| `materialization` | mapping | no | Whether generated resources are symlinks or regular files: `mode` and `targets`. See [Materialization](#materialization). |

`harnesses`, `allowed_registries`, and `allowed_marketplaces` must be lists of strings. Allowlist entries must compile as regular expressions.

## `harnesses`

Supported targets are:

| Value | Harness |
|---|---|
| `claude` | Claude Code |
| `factory` | Factory Droid |
| `cursor` | Cursor |
| `windsurf` | Windsurf |
| `gemini` | Gemini CLI |
| `devin` | Devin CLI |
| `codex` | Codex |

Target names are lowercase. Unknown targets fail with `v4.harness`.

A non-empty repository `harnesses` list takes precedence over the user and organization lists. If the repository file omits the field or sets an empty list, the user list is used; if that is also missing or empty, the organization list is used. Commands that only read the catalog, such as `list`, work without any target list and print a warning. `sync` needs a target list and exits with an error when none is configured.

Output paths for each harness are listed in the [harnesses and output paths reference](harnesses-and-output-paths.md).

## Source declarations

Each entry under `sources` describes one place to fetch resources from. The source name must match `[A-Za-z0-9][A-Za-z0-9._-]*` (`v4.source-name`).

```yaml
sources:
  shared:
    git: https://github.com/example-org/agent-resources.git
    ref: main
    path: skills
    source_harness: claude
  local:
    directory: ./agent-resources
```

A source must contain exactly one of `git`, `directory`, or `marketplace`; zero or two fail with `v4.source-discriminator`. All values are strings.

| Field | Applies to | Required | Description |
|---|---|---:|---|
| `git` | Git | yes | Git clone URL. |
| `ref` | Git | yes | Branch, tag, or commit to resolve. |
| `path` | Git | no | Subdirectory of the repository to discover resources in. |
| `source_harness` | Git, directory | no | Harness layout the source content is written in. Omit it to let the CLI detect the layout. |
| `directory` | Directory | yes | Folder on disk, read live. |
| `marketplace` | Marketplace | yes | Hosted Marketplace endpoint URL. |

Validation rules by kind:

| Rule | Diagnostic |
|---|---|
| Git source without `ref` | `v4.git-ref-required` |
| Invalid `ref` | `v4.git-ref` |
| `git` value that is a local path (`/`, `./`, `../`) instead of a remote URL, an scp-style origin such as `git@host:org/repo.git`, or a hostless absolute `file:///` URL | `v4.git-origin` |
| `path` that is absolute, contains `.` or `..` segments, or leaves the repository | `v4.git-path` |
| `ref` or `path` on a directory source | `v4.source-field` |
| `ref`, `path`, or `source_harness` on a Marketplace source | `v4.source-field` |
| `directory` path that leaves its base directory, or reaches outside it through a symlink | `v4.directory-path` |
| `marketplace` URL that is not `http` or `https`, has no host, carries user info, a query, or a fragment, or contains `.` or `..` path segments | `v4.marketplace-url` |
| `source_harness` that is not a supported harness id | `v4.harness` |
| Empty `git`, `directory`, or `marketplace` value | `v4.source-value` |

Use `directory` for a local folder; point it at the subfolder you want instead of using `path`.

In the repository `.agents/config.yaml` and `.agents/config.local.yaml`, `directory: .` selects the repository root and `directory: ./resources/skills` selects `<repo>/resources/skills`. User and organization files, included fragments, and files outside that layout resolve relative paths from their own directory. Relative paths, including through symlinks, must stay inside that base directory. Absolute paths are used as written. Earlier v4 releases resolved repository-root declarations from `.agents/`; a source stored under `.agents/` needs an explicit `./.agents/` prefix.

Two source names may point at the same URL, ref, and path. They stay separate in configuration, and the CLI fetches the content once.

## Resource groups

Every key under `resources` must be the name of a source declared in the same file or its includes. A group for an undeclared source fails with `v4.undefined-source`.

```yaml
resources:
  shared:
    include:
      - skill/code-review
      - command/release
    tags: [platform]
    exclude:
      - skill/legacy-review
    harnesses: [claude, cursor]
```

A plain list is shorthand for a group with only `include`:

```yaml
resources:
  shared:
    - skill/code-review
    - command/release
```

| Field | Type | Default | Description |
|---|---|---|---|
| `include` | resource-item list | nothing selected | Explicit resource requests. |
| `exclude` | exclusion list | nothing excluded | Removes matching selections that are not required. |
| `tags` | string list | no tag selection | Selects resources whose `metadata.tags` contain a listed tag. |
| `all` | boolean | `false` | Selects every resource of every kind in the source. |
| `registries` | string list | absent | Marketplace registry slugs that bound `all` and `tags`. Invalid for Git and directory sources (`v4.option-combination`). |
| `required` | boolean | `false` | Protects this group's selections from exclusions and from `remove`. It selects nothing by itself. |
| `harnesses` | string list | inherited | Narrows the target set for this group. It never adds a harness. |

String lists must not contain empty or duplicate values (`v4.duplicate`). `all` and `required` must be YAML booleans. Unknown group fields fail with `v4.unknown-field`.

Omitted fields leave inherited values alone when fragments merge. An explicit `tags: []` switches off tag selection inherited from a fragment, `all: false` switches off inherited `all`, and `harnesses: []` means no harness receives the group's resources.

For a Marketplace group with `all: true` or non-empty `tags`, `registries` must list at least one registry slug (`v4.dynamic-unbounded`). A `registries` list alone selects nothing.

### What each source kind supports

| Field or option | Git and directory | Marketplace |
|---|---|---|
| `include` | yes | yes |
| `exclude` | `*`, `<kind>/*`, or `<kind>/<name>` | exact `<registry-slug>/<kind>/<name>` only, not a collection |
| `tags`, `all` | yes | accepted by the parser, rejected at run time |
| `registries` | rejected | accepted by the parser, rejected at run time |
| `harnesses` on the group | yes | rejected at run time |
| `required` | yes | yes |
| `revision` option | rejected | yes |
| `as` option | only the resource's own name | yes, except for collections |

"Rejected at run time" means the file loads, but `sync` and other commands that resolve Marketplace resources fail with `v4.marketplace-runtime-unsupported`. Marketplace groups select through explicit `include` requests.

## Resource locators and options

A locator names one resource. Git and directory sources use `<kind>/<name>`. Marketplace sources use `<registry-slug>/<kind>/<name>`, because one endpoint serves many registries. The kinds are `skill`, `command`, `subagent`, `hook`, `mcp`, and `collection`. A locator in the wrong shape for its source fails with `v4.locator`.

Each `include` item is either a locator string or a mapping with one locator key and an options mapping:

```yaml
resources:
  shared:
    - command/deploy:
        as: deploy-platform
  company:
    - platform-tools/skill/review:
        revision: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
```

| Option | Type | Default | Description |
|---|---|---|---|
| `as` | string | resource name | Name to install the resource under. It must be a safe file name (`v4.installed-name`). Collections cannot use `as` because they do not install as themselves (`v4.option-combination`). |
| `revision` | string | no exact constraint | Exact Marketplace resource or collection revision, written `sha256:<64 lowercase hex>` (`v4.revision`). |

`revision` is invalid for Git and directory resources. Git resources use their source's `ref`; declare a second source to use a different ref. Semantic-version constraints and `latest` are not v4 options.

On Git and directory sources, `as` may be omitted or set to the resource's own name. A different name passes the parser but fails with `v4.runtime-option` when the configuration is resolved.

Listing the same locator twice in one include list fails with `v4.duplicate-request`, even when the options match. An option value that is empty, null, or not a string fails with `v4.resource-options`, and a mapping item with more than one key fails with `v4.resource-item`. Unknown option names fail with `v4.unknown-field`.

## Selection and exclusions

A group's candidates are the resources named by `include`, matched by `tags`, or covered by `all`. Collections then expand into their members, and exclusions remove anything that is not required. When a resource is both requested explicitly and selected by `tags` or `all`, the explicit request's options apply.

Within one file, exclusions apply after inclusions, whatever order the keys appear in. Across an include tree, order matters: an include in a later file brings back a resource that an earlier fragment excluded. Required resources are never excluded.

`required: true` protects the group's selections but is not a selection by itself, so a group that contains only `required: true` selects nothing. Marketplace resources selected by an organization group are not required unless that group sets `required: true`; when it does, the protection covers its direct pins and the members of any collection it selects.

Collections expand to their members before resources are resolved. Collections themselves are never written into harness folders. See the [collections reference](collections.md) for the collection file format.

## Hosted Marketplaces

Declare a hosted [Marketplace](../glossary.md#marketplace) as a named source and request resources from it in a group:

```yaml
sources:
  company:
    marketplace: https://marketplace.example.com
resources:
  company:
    include:
      - shared-agents/skill/review:
          revision: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

Installing and resolving Marketplace resources requires a verified lock record in the owning layer's lock that matches the requested endpoint, registry, resource, and revision. A missing or unusable lock fails with `v4.marketplace-lock-required`. A lock with no record for the endpoint, or with a record that does not match, fails with `v4.marketplace-lock-mismatch`. Either way nothing is installed. One layer can have only one source that fetches from a given endpoint.

Marketplace exclusions are exact `registry-slug/kind/name` entries under `resources.<source>.exclude`, and they win over inclusions: a group that includes a collection and excludes one of its members installs only the remaining members. The excluded resource is never fetched, so it cannot fail a sync.

A Marketplace group that contains only `exclude` makes its source exclusion-only. Such a source fetches nothing, needs no lock record, and hides optional resources that the organization file selects from the same endpoint. Exclusion-only Marketplace sources are allowed only in the user file; elsewhere they fail with `v4.layer-field`. Exclusions never hide `required: true` pins, and apart from this user override of organization pins, they never hide another layer's selections. Because the user file applies to the whole machine, the override does too. For example:

```yaml
apiVersion: agent-marketplace/v4
sources:
  company:
    marketplace: https://marketplace.example.com
resources:
  company:
    exclude: [shared-agents/skill/review]
```

See [How to install a resource from a hosted Marketplace](../how-to/install-from-a-marketplace.md) for the full workflow.

`allowed_marketplaces` holds regular expressions matched against normalized Marketplace endpoint URLs (lowercase scheme and host, no trailing slash). The first layer that declares it, checked in the order organization, user, repository, controls the policy. The personal-local file cannot declare it.

## Registry allowlist

`allowed_registries` holds regular expressions matched against normalized Git origins. Before matching, the CLI removes an `https://`, `http://`, `git://`, or `ssh://` prefix, turns a leading `git@host:` into `host/`, and strips trailing slashes and a trailing `.git`. For example, `git@github.com:example-org/tools.git` is matched as `github.com/example-org/tools`.

```yaml
allowed_registries:
  - '^github\.com/example-org/'
```

The first layer that declares the field controls it, checked in the order organization, user, repository. Within that layer, the first declaration in include order wins. The personal-local file cannot declare it. Every `git` source is checked, including `file://` origins. Directory sources are not checked.

| Configuration | Effect |
|---|---|
| field omitted in every layer | All Git origins are allowed. |
| `allowed_registries: []` | All Git origins are denied. |
| one or more patterns | A Git origin must match at least one pattern. |

## Materialization

`materialization` controls how sync writes skills, commands, and subagents into harness folders (the product calls this step [materialize](../glossary.md#materialize)):

```yaml
materialization:
  mode: symlink
```

| Mode | Behavior |
|---|---|
| `symlink` | Default. Skills, commands, and subagents are [symlinks](../glossary.md#symlink) into the compiled cache under `~/.cache/agent-marketplace/`. Factory commands are always regular files. |
| `file` | Writes regular files and folders for every target. The CLI tracks what it wrote in a materialization manifest. |

Use `targets` when one harness needs a different mode:

```yaml
materialization:
  mode: file
  targets:
    devin: symlink
```

Mode values are `symlink` or `file`; target keys must be supported harness ids.

Which scope a setting affects depends on the file:

| File | Repository-scoped output | Machine-scoped output |
|---|---|---|
| User | yes (default for both scopes) | yes |
| Repository | yes | no |
| Repository personal | yes | no |
| Organization | not allowed (`v4.layer-field`) | not allowed |

Every harness supports both modes. Shared files such as hook and MCP configuration are always regular files edited in place, and their ownership rules apply in either mode.

## Name collisions

When sources with different origins or refs select resources with the same kind and name, Agent Marketplace picks one winner and prints a warning. A resource from a `required: true` group wins first, then the one from the higher-precedence layer or file, then the one with the alphabetically first normalized origin. Configuration order breaks any remaining tie.

Hooks are the exception: hooks from independent sources are all installed, even when their names match. Repeated references to the same source are installed once.

Different kinds never collide. A skill and a command may share a name.

The [scopes, selection, and precedence explanation](../explanation/scopes-selection-and-precedence.md) covers collisions, exclusions across layers, and removal in more detail.
