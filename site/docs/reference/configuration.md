# Repository configuration reference

The committed `.agents/config.yaml` file declares which sources a repository uses, which resources it selects, and which coding harnesses receive those resources. Agent Marketplace uses the v4 source/resource schema for every configuration layer. A configuration file can compose local YAML fragments with the `include` field.

For task-oriented guidance, see [How to adopt agent-marketplace in a repository](../how-to/adopt-agent-marketplace-in-a-repository.md) and [How to keep resources reproducible](../how-to/keep-resources-reproducible.md).

Agent Marketplace treats configuration as the source of selection policy and generated layer locks as the exact Git acquisition record. Normal readers adapt v4 Git and directory sources to the runtime requirement model. Configuration editors that do not understand v4 refuse to rewrite a v4 root; edit the `sources` and `resources` declarations directly.

## File location and scope

Agent Marketplace searches upward from the current directory and uses the nearest `.agents/config.yaml`. It does not merge multiple repository configuration files found at different directory levels.

The selected file applies to its directory tree. Generated repository-scoped output is written next to that file.

The four configuration layers are:

| Layer | Configuration | Generated acquisition lock | Purpose |
|---|---|---|---|
| Organization | `/etc/agent-marketplace/config.yaml` | `/etc/agent-marketplace/lock.yaml` | Managed machine policy and required resources |
| User | `~/.agents/config.yaml` | `~/.agents/lock.yaml` | Personal machine-wide targets and resources |
| Repository | `<repo>/.agents/config.yaml` | `<repo>/.agents/lock.yaml` | Shared project targets and resources |
| Repository personal | `<repo>/.agents/config.local.yaml` | `<repo>/.agents/lock.local.yaml` | Gitignored choices for one checkout |

Commit the repository lock with its configuration. Keep the personal-local lock ignored. Consumer commands never create an absent organization lock. Plain directory sources are live and do not appear in Git acquisition locks.

## Schema

A v4 root must declare exactly `agent-marketplace/v4`:

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

The `sources` map contains named source declarations. The `resources` map binds selection policy to those source names. Source names are local configuration references, not durable acquisition identities and not installation scopes.

A source declares exactly one acquisition kind:

- `git` is a remote Git repository and requires `ref`.
- `directory` is a live local directory and is resolved relative to the file that declares it.
- `marketplace` is a hosted Marketplace endpoint.

A directory source is explicit; it is not inferred from a `.git` suffix. A v4 parser rejects unsupported installation keys, unknown fields, YAML aliases, merge keys, duplicate keys, and multiple YAML documents.

V4 roots require `apiVersion`, including `config.local.yaml`. Included fragments may omit it and inherit v4 from their containing tree. An explicit non-v4 version in a fragment is rejected.

## Configuration layers

The effective layer order, from lowest to highest precedence, is organization, user, repository, and repository personal. Top-level harness targets use project-first precedence: a non-empty repository `harnesses` list wins, then the user list, then the organization list. A resource group's `harnesses` list narrows the effective target set and cannot add a target that is not already selected.

Layer-specific restrictions still apply:

- `telemetry` is allowed only in organization and user configuration.
- `allowed_registries` and `allowed_marketplaces` are controlled by organization, user, and repository configuration; the personal-local layer cannot declare them.
- Repository personal configuration is for checkout-local resource policy and materialization overrides.

## `include`

`include` composes local YAML files into the configuration layer that declares it. It accepts one path or an ordered list of paths:

```yaml
apiVersion: agent-marketplace/v4
include: profiles/company.yaml
```

Use a sequence when including more than one fragment:

```yaml
include:
  - profiles/company.yaml
  - profiles/payments-team.yaml
```

Relative paths resolve from the directory containing the file that declares them. Absolute paths are also accepted. Includes are recursive. Shared fragments expand once per include branch; only an ancestor repeated on the current branch is a cycle. Glob patterns and remote URLs are not supported.

Included fragments have lower precedence than their containing file. For an ordered list, the first fragment is merged first, later fragments merge over earlier fragments, and the containing file merges last. Field-specific merge rules still apply; `include` does not make every field a simple replacement.

The loader rejects a layer when an include is missing, unreadable, malformed, invalid for that layer, or part of a direct or indirect cycle. Included fragments may omit `apiVersion`, but they must use fields allowed by the layer that includes them. Configuration-editing commands modify only the selected root and do not rewrite included fragments.

## Root fields

Unknown top-level fields are rejected.

| Field | Type | Required | Description |
|---|---|---:|---|
| `include` | path or list of paths | no | Local YAML fragments composed into this configuration layer. Relative paths resolve from the declaring file. |
| `apiVersion` | string | yes for roots | Must equal `agent-marketplace/v4`. |
| `harnesses` | list of strings | no | Target harnesses. When omitted or empty, targets come from user configuration, then organization configuration. |
| `sources` | mapping | no | Named Git, directory, or Marketplace source declarations. |
| `resources` | mapping | no | Resource groups bound to named sources. |
| `allowed_marketplaces` | list of regular expressions | no | Restricts hosted Marketplace endpoint URLs. |
| `allowed_registries` | list of regular expressions | no | Restricts remote Git origins. |
| `backup` | mapping | no | Controls configured pre-sync backups. |
| `logging` | mapping | no | Controls user-level operational logging where allowed. |
| `telemetry` | mapping | no | Machine-scope telemetry policy where allowed. |
| `materialization` | mapping | no | Controls whether generated output uses links or regular files. |

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

Target names are lowercase. Unknown targets are rejected.

A non-empty repository `harnesses` list takes precedence over the user and organization lists. If the repository omits the field or sets an empty list, the user list is used; if that is also absent or empty, the organization list is used.

## Source declarations

Each item under `sources` identifies one source location. The source name must match `[A-Za-z0-9][A-Za-z0-9._-]*`.

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

A source declaration must contain exactly one of `git`, `directory`, or `marketplace`.

| Field | Type | Applies to | Required | Description |
|---|---|---|---:|---|
| `git` | string | Git | yes | Git clone URL. |
| `ref` | string | Git | yes | Branch, tag, or commit to fetch. |
| `path` | string | Git | no | Relative subdirectory within the Git source. |
| `source_harness` | string | Git, directory | no | Layout used by the source content. Omit to autodetect. |
| `directory` | string | Directory | yes | Live local directory, resolved relative to the declaring file. |
| `marketplace` | string | Marketplace | yes | Hosted Marketplace endpoint URL. |

Fields belonging to another source kind are rejected. Marketplace sources do not accept Git-only fields. Directory sources do not acquire a Git lock.

Two source names may refer to equivalent declarations. They remain separate configuration bindings while compatible Git acquisition can be shared internally.

## Resource groups

Every resource group is keyed by a source name in the same root and include tree:

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

A compact list is equivalent to an expanded group with `include`:

```yaml
resources:
  shared:
    - skill/code-review
    - command/release
```

| Field | Type | Default | Description |
|---|---|---|---|
| `include` | resource-item list | no contributions | Explicit resource requests. |
| `exclude` | exclusion-string list | no exclusions | Removes matching non-required selections. |
| `tags` | string list | no tag selection | Selects resources whose metadata contains a matching tag. |
| `all` | boolean | `false` | Selects every supported resource kind in the applicable source scope. |
| `registries` | string list | absent | Marketplace registry slugs bounding dynamic selection. Invalid for Git and directory sources. |
| `required` | boolean | `false` | Protects selections made by this group from exclusions and consumer removal. It selects nothing by itself. |
| `harnesses` | string list | inherited | Narrows the applicable target set and never widens it. |

Omitted fields contribute no override during include composition. Explicit empty `tags: []` disables inherited tag selection. Explicit `all: false` disables inherited all-selection. Explicit `harnesses: []` selects no targets.

For a Marketplace group with `all: true` or non-empty `tags`, `registries` must contain at least one explicit registry slug. A `registries` list alone selects nothing. Explicit resource requests may name a different registry when the locator is fully qualified by the Marketplace source contract.

## Resource locators and options

Git and directory sources use `<kind>/<name>` locators. Marketplace sources use `<registry-slug>/<kind>/<name>` locators. Supported kinds are `skill`, `command`, `subagent`, `hook`, `mcp`, and `collection`.

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
| `as` | string | resource name | Installed name, subject to path validation. Collections cannot use `as` because they do not materialize. |
| `revision` | string | no exact constraint | Exact `sha256:<64 lowercase hex>` Marketplace resource or collection revision. |

`revision` is invalid for Git and directory resources. Git resources share their source's requested `ref`; use separate source declarations for independent refs. Semantic-version constraints and `latest` are not v4 options.

For Git and directory sources, the runtime accepts an omitted or same-name `as` value. A different alias is parser-valid but rejected with `v4.runtime-option` until the runtime supports it.

Duplicate locators within one include list are rejected, even when their options match. Null or scalar option values and multi-key resource items are invalid.

## Selection and exclusions

Selection candidates are the union of explicit requests, tag matches, and `all` selection, followed by collection expansion and exclusions. An explicit request supplies its options when a resource is also selected dynamically. A later explicit include can re-enable a resource suppressed by an earlier exclusion, except for required-policy protections.

A `required: true` group protects its selections but is not itself a selection expression. A required-only group therefore selects zero catalog resources.

Collections expand to their members before resources are resolved. Collections themselves are not materialized into harness directories. See the [collections reference](collections.md) for the registry-side collection file format.

## Hosted Marketplaces

Declare a hosted Marketplace as a named source and bind requests to it through a resource group:

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

Marketplace installation and runtime resolution require an exact, verified layer lock for the requested endpoint, registry, resource, and revision. Missing or mismatched lock evidence fails closed. Use [How to install a resource from a hosted Marketplace](../how-to/install-from-a-marketplace.md) for the end-to-end workflow.

`allowed_marketplaces` contains regular expressions matched against normalized Marketplace endpoint URLs. The first layer that declares it, in organization-to-repository authority order, controls the policy; the personal-local layer cannot declare it.

## Registry allowlist

`allowed_registries` contains regular expressions matched against normalized remote origins. URL schemes, an SSH `git@` prefix, and a trailing `.git` suffix are removed before matching.

```yaml
allowed_registries:
  - '^github\.com/example-org/'
```

The first configuration authority that declares the field controls it: organization, then user, then repository. Local directory sources are not checked against this allowlist.

| Configuration | Effect |
|---|---|
| field omitted | All remote origins are allowed unless another layer declares a policy. |
| `allowed_registries: []` | All remote origins are denied. |
| one or more patterns | A remote origin must match at least one expression. |

## Materialization

Repository configuration supports two output modes:

```yaml
materialization:
  mode: symlink
```

| Mode | Behavior |
|---|---|
| `symlink` | Default. Links generated resources to the compiled source cache. |
| `file` | Writes regular files and directory trees. |

A repository setting affects project-scoped output only. Use `targets` when one harness needs a different mode:

```yaml
materialization:
  mode: file
  targets:
    devin: symlink
```

All targets support both modes through the generic materializer. Shared hooks and MCP configuration retain their ownership rules in either mode; this setting controls generated resource files and trees.

## Name collisions

When different source origins or refs select resources with the same kind and name, Agent Marketplace chooses a deterministic winner and reports a warning. A required source wins first, followed by the resource from the higher-precedence configuration layer, then the resource with the alphabetically first normalized origin. Stable configuration order breaks any remaining tie.

Names shared by different resource kinds are independent. For example, a skill and a command may have the same name.
