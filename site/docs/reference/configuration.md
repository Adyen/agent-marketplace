# Repository configuration reference

The committed `.agents/config.yaml` file defines which sources a repository uses, which resources it selects, and which coding harnesses receive those resources. Legacy v2/v3 configuration uses `registries` and per-kind selectors; v4 uses named `sources` and `resources`. A configuration file can compose local YAML fragments with the `include` field.

For task-oriented guidance, see [How to adopt agent-marketplace in a repository](../how-to/adopt-agent-marketplace-in-a-repository.md) and [How to keep resources reproducible](../how-to/keep-resources-reproducible.md).

Agent Marketplace treats configuration as the source of selection policy and generated layer locks as the exact Git acquisition record. Normal readers accept v4 roots and adapt them for discovery and synchronization. After editing a legacy root, run `agent-marketplace sync` to reconcile its writable lock and generated harness files, or run `agent-marketplace sync --check` to report lock and output drift without changing files. Legacy config writers refuse to edit a v4 root; use an explicit v4-aware operation instead.

## File location and scope

Agent Marketplace searches upward from the current directory and uses the nearest `.agents/config.yaml`. It does not merge multiple repository configuration files found at different directory levels.

The selected file applies to its directory tree. Generated repository-scoped output is written next to that file.

Other configuration layers use related schemas:

| Layer | Configuration | Generated Git lock | Purpose |
|---|---|---|---|
| Organization | `/etc/agent-marketplace/config.yaml` | `/etc/agent-marketplace/lock.yaml` | Managed machine policy and required resources |
| User | `~/.agents/config.yaml` | `~/.agents/lock.yaml` | Personal machine-wide targets and resources |
| Repository | `<repo>/.agents/config.yaml` | `<repo>/.agents/lock.yaml` | Shared project targets and resources |
| Repository personal | `<repo>/.agents/config.local.yaml` | `<repo>/.agents/lock.local.yaml` | Gitignored choices for one checkout |

Commit the repository lock with its configuration. The personal-local lock is ignored, and consumer commands never create an absent organization lock. Plain directory registries are live and do not appear in Git locks. Hosted Marketplace resources use exact pins under `marketplaces[].resources` and do not appear in the generated Git lockfiles.

Fields that are exclusive to organization, user, or repository-personal configuration are outside its scope. The `include` field is available in all four layers and follows the rules below.

## Schema versions and v4 compatibility

Legacy configuration documents use `apiVersion: agent-marketplace/v2`; `agent-marketplace/v3` remains a read-compatible alias. V4 is an explicit source/resource schema:

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

A v4 root must declare exactly `agent-marketplace/v4`. Its `sources` map names explicit Git, live directory, or Marketplace sources, and its `resources` map binds typed locators and selection policy to those source names. Use exactly one source discriminator: `git` requires `ref`, `directory` is live and relative to the declaring file, and `marketplace` accepts no Git-only fields. A directory source is not inferred from a `.git` suffix. V4 accepts local include trees; a child fragment may omit `apiVersion`, but an explicit non-v4 version or legacy key is rejected rather than guessed.

Normal CLI reads—including discovery, status, synchronization, and configuration loading—recognize v4 and adapt Git/directory sources to the runtime requirement model. Marketplace sources are syntax-validated but are not runtime-resolvable on this branch: the legacy runtime adapter always returns `v4.marketplace-lock-required` because hosted acquisition-lock integration is not implemented. Effective staged reloads consume v4 root/include bytes from in-memory overlays without changing disk files. Legacy editors refuse with `v4.writer-opt-in`; repository-personal v4 roots reject disallowed fields with `v4.layer-field`.

To convert legacy declarations, run `agent-marketplace migrate --to v4`. The command validates all writable user, repository, and repository-personal layers by default; `--scope user|repo|local` selects one. Organization configuration is never a migration target, and migration does not change lockfiles. Use `--dry-run` to preview. Conversion is fail-closed: legacy include trees and hosted Marketplace selections without proven pin provenance are refused, and a failed conversion leaves the input files unchanged. See the [CLI migration reference](cli.md#migrate).

## `include`

`include` composes local YAML files into the configuration layer that declares it. It accepts one path or an ordered list of paths:

```yaml
apiVersion: agent-marketplace/v2
include: profiles/company.yaml
```

Use a sequence when including more than one fragment:

```yaml
include:
  - profiles/company.yaml
  - profiles/payments-team.yaml
```

Relative paths resolve from the directory containing the file that declares them. Absolute paths are also accepted. Includes are recursive, so an included fragment may include other fragments. In legacy include trees, the same file contributes only at its first position when reached more than once; canonical paths are used for this check. V4 include trees retain branch-local ancestry instead: shared fragments expand once per include branch, and only an ancestor repeated on the current branch is a cycle. Glob patterns and remote URLs are not supported.

Included fragments have lower precedence than their containing file. For an ordered list, the first fragment is merged first, later fragments merge over earlier fragments, and the containing file merges last. The normal field-specific merge rules still apply; `include` does not make every field a simple replacement. See the [selection and precedence reference](https://github.com/Adyen/agent-marketplace/blob/main/docs/PRECEDENCE.md) for the complete merge rules.

The loader rejects a layer when an include is missing, unreadable, malformed, invalid for that layer, or part of a direct or indirect cycle. Included fragments may omit `apiVersion`, but they must use fields allowed by the layer that includes them. A regular organization, user, or repository legacy root still requires `apiVersion: agent-marketplace/v2` (or the accepted `agent-marketplace/v3` compatibility alias); a legacy local root may omit it. A v4 root requires `apiVersion: agent-marketplace/v4`; v4 fragments may omit the version but cannot declare v2/v3 or legacy installation keys.

The CLI reads included content when calculating effective configuration. Legacy configuration-editing commands modify only the selected root file and do not rewrite included fragments; they refuse rather than rewriting a v4 root. Removing an include-owned resource records the exclusion in the selected root, leaving the included file unchanged.

## Example

```yaml
apiVersion: agent-marketplace/v2
harnesses:
  - claude
  - cursor

allowed_registries:
  - '^github\.com/example-org/'

materialization:
  mode: symlink

registries:
  - url: https://github.com/example-org/agent-resources.git
    ref: v1.4.0
    path: resources
    source_harness: claude
    harnesses:
      - claude
    skills:
      include:
        - code-review
      exclude:
        - legacy-review
    commands:
      include:
        - release
    tags:
      - security
```

## Top-level fields

Unknown top-level fields are rejected.

| Field | Type | Required | Description |
|---|---|---:|---|
| `include` | path or list of paths | no | Local YAML fragments composed into this configuration layer. Relative paths resolve from the declaring file; absolute paths are accepted. |
| `apiVersion` | string | yes | Configuration schema. Legacy roots use `agent-marketplace/v2`; v4 roots use `agent-marketplace/v4`. |
| `harnesses` | list of strings | no | Repository target harnesses. When omitted or empty, targets come from user configuration, then organization configuration. |
| `registries` | list of registry entries | no | Legacy v2/v3 Git repositories or local directories that provide resources. Not valid in a v4 document. |
| `sources` | mapping | no | V4 named Git, directory, or Marketplace source declarations. Not valid in a legacy document. |
| `resources` | mapping | no | V4 resource groups bound to named sources. Not valid in a legacy document. |
| `marketplaces` | list of Marketplace entries | no | Legacy v2/v3 hosted Marketplace endpoints and exact installed resource pins. Entries accumulate across configuration layers; not valid in a v4 document. |
| `allowed_marketplaces` | list of regular expressions | no | Restricts hosted Marketplace endpoint URLs. The first layer that declares the field, from organization downward, controls the policy. |
| `skills` | selector | no | Default skill selection for registry entries that do not provide their own values. |
| `commands` | selector | no | Default command selection. |
| `subagents` | selector | no | Default subagent selection. |
| `hooks` | selector | no | Default hook selection. |
| `mcp` | selector | no | Default MCP server selection. |
| `collections` | selector | no | Default collection selection. Collections expand to their member resources. |
| `tags` | list of strings | no | Default metadata tags used to select resources. |
| `allowed_registries` | list of regular expressions | no | Restricts remote registry origins when no higher-authority layer defines the policy. |
| `materialization` | mapping | no | Controls whether repository output uses links or regular files. |

### `apiVersion`

Use the legacy canonical value for a v2/v3 document:

```yaml
apiVersion: agent-marketplace/v2
```

`agent-marketplace/v3` is accepted as a compatibility alias. A repository configuration without `apiVersion` is rejected. A v4 document must instead use the exact value:

```yaml
apiVersion: agent-marketplace/v4
```

V4 replaces `registries` and per-kind top-level selectors with named `sources` and `resources`; do not mix the two document shapes. The v4 parser rejects legacy installation keys and unknown fields rather than dropping them.

### V4 resource groups

A resource group may be compact or expanded with `include`, `exclude`, `tags`, `all`, `registries`, `required`, and `harnesses`. `registries` is valid only for Marketplace sources and is required with a non-empty list when `all: true` or non-empty `tags` would otherwise select dynamically. Named `include` and `exclude` decisions accumulate across fragments; presence-aware policy fields such as `tags: []` and `harnesses: []` explicitly replace inherited values. `required: true` protects selections but selects nothing by itself; ordered exclusions can be overridden by a later explicit include.

Resource items may use an options mapping with `as` or Marketplace-only `revision`. The current legacy runtime rejects non-default `as` aliases with `v4.runtime-option` rather than silently materializing under the wrong name; aliases are therefore parser-valid but not a supported Git/directory runtime feature in this slice.

### `harnesses`

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

Target names are lowercase. Unknown targets are rejected.

A non-empty repository `harnesses` list takes precedence over the user and organization lists. If the repository omits the field or sets an empty list, the user list is used; if that is also absent or empty, the organization list is used.

## Registry entries

Each item under `registries` identifies one source location and controls what Agent Marketplace selects from it.

```yaml
registries:
  - url: https://github.com/example-org/agent-resources.git
    ref: main
    path: skills
    source_harness: claude
    skills:
      include:
        - code-review
```

Unknown registry-entry fields are rejected.

| Field | Type | Required | Description |
|---|---|---:|---|
| `url` | string | yes | Git clone URL or local directory. A local registry may use `path` instead as shorthand. |
| `ref` | string | Git only | Git branch, tag, or commit to fetch. Not required for local directories. |
| `path` | string | no | Subdirectory within a Git registry when `url` is present. Must be relative and remain inside the registry root. |
| `source_harness` | string | no | Layout used by the registry content. Omit to autodetect. |
| `harnesses` | list of strings | no | Narrows this entry to targets also present in the effective top-level target set. |
| `required` | boolean | no | Makes resources immune to exclusions. When the entry has no explicit includes or tags, selects all of its resources. Intended for organization policy. |
| `default` | boolean | no | When the entry has no explicit includes or tags, selects all resources and then applies exclusions. |
| `exclude_all` | boolean | no | Excludes every non-required resource kind from this registry entry. |
| `skills` | selector | no | Skill selection for this entry. |
| `commands` | selector | no | Command selection for this entry. |
| `subagents` | selector | no | Subagent selection for this entry. |
| `hooks` | selector | no | Hook selection for this entry. |
| `mcp` | selector | no | MCP server selection for this entry. |
| `collections` | selector | no | Collection selection for this entry. |
| `tags` | list of strings | no | Selects resources whose metadata contains a matching tag. |

A Git registry must have both `url` and `ref`:

```yaml
registries:
  - url: https://github.com/anthropics/skills.git
    ref: main
    path: skills/frontend-design
```

`path` cannot be absolute or contain a parent-directory escape. The tuple of `url`, `ref`, and `path` identifies the configured source location.

### Local registries

Local registries can use an absolute path, a home-relative path, `.`, or a path beginning with `./`:

```yaml
registries:
  - path: ./agent-resources
    skills:
      include:
        - local-review
```

When a registry entry has no `url`, `path` is treated as the local registry location rather than as a subdirectory. Relative local registries resolve from the directory containing `.agents/config.yaml` and cannot escape it. User-wide configuration cannot contain repository-relative registry paths because their meaning would change between repositories.

### `source_harness`

`source_harness` accepts the same seven harness names listed under [`harnesses`](#harnesses). It describes the registry's input layout; it does not choose the output target.

Omit it when the registry contains enough layout markers for automatic detection. Set it when the content is ambiguous:

```yaml
source_harness: claude
```

### Registry target narrowing

A registry-level `harnesses` list intersects with the effective top-level list. It cannot add a target that the effective list did not select.

```yaml
harnesses: [claude, cursor]
registries:
  - url: https://github.com/example-org/claude-only.git
    ref: main
    harnesses: [claude]
```

In this example, resources from `claude-only` are emitted only for Claude Code.

## Resource selectors

The following fields all use the same selector shape:

- `skills`
- `commands`
- `subagents`
- `hooks`
- `mcp`
- `collections`

The canonical map form has three fields:

| Field | Type | Description |
|---|---|---|
| `include` | list of names | Selects resources by exact name. |
| `exclude` | list of names | Removes matching non-required resources from the selection. |
| `exclude_all` | boolean | Removes every non-required resource of this kind. |

```yaml
skills:
  include:
    - code-review
    - frontend-design
  exclude:
    - legacy-review
  exclude_all: false
```

A list is accepted as shorthand for `include`:

```yaml
skills:
  - code-review
  - frontend-design
```

When Agent Marketplace rewrites a selector, it uses the canonical map form.

Top-level selectors provide defaults to registry entries. A selector value declared on an entry overrides the corresponding inherited list. `exclude_all: true` remains in force if either the top level or the registry entry enables it.

An entry with no selectors, tags, collections, `required`, or `default` contributes resources to `agent-marketplace list` but does not select anything for materialization.

## Tag selection

`tags` selects resources by their `metadata.tags` values:

```yaml
registries:
  - url: https://github.com/example-org/agent-resources.git
    ref: main
    tags:
      - security
      - frontend
```

Tag matching is exact after surrounding whitespace is removed. Tags add to explicit `include` selections; the applicable `exclude` rules are evaluated afterward.

## Collections

Collections are named bundles defined by a registry. Select them with the same selector syntax:

```yaml
collections:
  include:
    - platform-onboarding
```

A selected collection expands to its members before resources are resolved. Collections themselves are not materialized into harness directories.

## Hosted Marketplaces

`marketplaces` records hosted Marketplace endpoints and immutable resource pins in the legacy v2/v3 configuration shape. It is not a v4 top-level field. The explicit v2/v3-to-v4 migration refuses files containing these selections because a saved pin does not by itself prove the provenance required to recreate a v4 Marketplace request and acquisition record. Keep the legacy file unchanged and remediate the Marketplace selections separately. The legacy shape is:

```yaml
marketplaces:
  - url: https://marketplace.example.com
    instance_id: team-marketplace
    alias: team
    resources:
      - registry_id: shared-agents-id
        registry_slug: shared-agents
        kind: skill
        name: review
        revision: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
        digest: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
        acquisition: mirror
        installed_name: team-review
        direct: true
```

Marketplace entries accumulate across organization, user, repository, and repository-personal layers. `agent-marketplace install <endpoint-url>` verifies and writes an endpoint entry. Installing a Marketplace locator writes its exact resource pins; hand-editing pin identity or digest fields bypasses the installation preview and is not equivalent to a verified install.

### Marketplace entry fields

| Field | Type | Required | Description |
|---|---|:---:|---|
| `url` | HTTP(S) endpoint URL | yes | Canonical Marketplace API base. Trailing slashes and scheme/host casing are normalized for identity comparisons. |
| `instance_id` | route token | yes | Stable identity verified against `/.well-known/agent-marketplace.json`. |
| `alias` | route token | no | Unique user-facing name for search and compact locators. `all` is reserved. |
| `resources` | list of exact pins | no | Installed resource revisions owned by this endpoint. |

An alias cannot contain `/`, `\\`, `@`, `:`, control characters, or leading or trailing whitespace. Duplicate non-empty aliases are rejected.

### `marketplaces[].resources[]`

| Field | Type | Required | Description |
|---|---|:---:|---|
| `registry_id` | route token | yes | Immutable registry identity. |
| `registry_slug` | route token | no | Route-facing registry slug used by mirror endpoints and display output. |
| `kind` | string | yes | `skill`, `command`, `subagent`, `hook`, `mcp`, or `collection`. |
| `name` | route token | yes | Marketplace resource name. |
| `revision` | string | yes | Exact revision identifier. Newly installed resources use the content digest. |
| `digest` | SHA-256 digest | yes | Lowercase `sha256:<64 hex>` content digest. |
| `acquisition` | string | no | `mirror` or `index-only`. An omitted value is treated as `mirror`. |
| `registry_commit` | Git commit | for `index-only` | Full lowercase commit for the exact registry occurrence. Forbidden on mirror pins. |
| `registry_manifest_digest` | SHA-256 digest | for `index-only` | Manifest digest for the exact registry occurrence. Forbidden on mirror pins. |
| `installed_name` | safe filename | no | Local generated name. Defaults to `name`; path separators, control characters, platform-invalid names, and trailing dots/spaces are rejected. |
| `direct` | boolean | no | Marks a resource selected directly rather than only through a collection. |
| `selected_by` | list of collection selectors | no | Exact collection revisions that retain this leaf. |

Each `selected_by` item contains `registry_id`, `name`, and a lowercase SHA-256 `revision`. A leaf remains selected while it is direct or retained by at least one installed collection.

### Marketplace allowlist

`allowed_marketplaces` contains regular expressions matched against normalized Marketplace endpoint URLs:

```yaml
allowed_marketplaces:
  - '^https://marketplace\.example\.com$'
```

The first layer that declares this field controls it, in authority order: organization, user, repository. `LocalConfig` cannot declare this authority. Lower-authority layers cannot relax a policy selected by a higher-authority layer.

| Configuration | Effect |
|---|---|
| field omitted in every layer | All syntactically valid HTTP(S) Marketplace endpoints are allowed. |
| `allowed_marketplaces: []` | All Marketplace endpoints are denied. |
| one or more patterns | An endpoint must match at least one expression. |

The policy is checked before endpoint registration, search, acquisition, and synchronization.

For the workflow that creates these entries, see [How to install a resource from a hosted Marketplace](../how-to/install-from-a-marketplace.md) and the [Marketplace resource locators reference](./marketplace-resource-locators.md).

## Registry allowlist

`allowed_registries` contains regular expressions matched against normalized remote origins. URL schemes, an SSH `git@` prefix, and a trailing `.git` suffix are removed before matching.

```yaml
allowed_registries:
  - '^github\.com/example-org/'
```

The first configuration authority that declares the field controls it: organization, then user, then repository. A repository allowlist therefore applies only when neither organization nor user configuration defines one.

The three states are distinct:

| Configuration | Effect |
|---|---|
| field omitted | All remote origins are allowed unless another layer declares a policy. |
| `allowed_registries: []` | All remote origins are denied. |
| one or more patterns | A remote origin must match at least one pattern. |

Local registries are not checked against this allowlist.

## Materialization

Repository configuration supports two output modes:

```yaml
materialization:
  mode: symlink
```

| Mode | Behavior |
|---|---|
| `symlink` | Default. Links generated resources to the compiled registry cache. |
| `file` | Writes regular files and directory trees. |

A repository setting affects project-scoped output only. In file mode, Agent Marketplace records managed paths in `.agents/materialization-manifest.yaml` so stale files can be reconciled safely.

Use `targets` when one harness needs a different mode from the scope default:

```yaml
materialization:
  mode: file
  targets:
    devin: symlink
```

Target overrides follow the same layer and scope precedence as `mode`. All targets, including Devin, support both modes through the generic materializer. Shared hooks and MCP configuration retain their existing reconciliation and ownership rules in either mode; this setting controls generated resource files and trees, not shared-entry ownership.

## Name collisions

When different registry origins or refs select resources with the same kind and name, Agent Marketplace chooses a deterministic winner and reports a warning. A required registry wins first, followed by the resource from the higher-precedence configuration layer, then the resource with the alphabetically first normalized registry URL. Stable configuration order breaks any remaining tie.

Names shared by different resource kinds are independent. For example, a skill and a command may have the same name.
