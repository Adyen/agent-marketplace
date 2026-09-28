# Lockfiles reference

A lockfile records exactly what a configuration file resolved to: the Git [commit](../glossary.md#commit) behind each Git source's ref, and the exact revision of each Marketplace resource. With the lock committed, everyone who runs `sync` on the repository gets the same files, even after a branch or tag moves. This page describes where lockfiles live, what they contain, and which commands write them. Agent Marketplace generates them; you do not write them by hand.

## Layer-owned lockfiles

Each configuration file has its own lock in the same folder. A lock records only the sources that its configuration file declares.

| Layer | Configuration | Generated lock |
|---|---|---|
| Organization | `/etc/agent-marketplace/config.yaml` | `/etc/agent-marketplace/lock.yaml` |
| User | `~/.agents/config.yaml` | `~/.agents/lock.yaml` |
| Repository | `<repo>/.agents/config.yaml` | `<repo>/.agents/lock.yaml` |
| Repository personal | `<repo>/.agents/config.local.yaml` | `<repo>/.agents/lock.local.yaml` |

Commit the repository lock together with `.agents/config.yaml`. Keep the personal-local lock out of Git; the CLI adds `.agents/lock.local.yaml` to `.gitignore` when `.agents/config.local.yaml` exists.

Directory sources are read live from disk on every run and never appear in a lock.

### Organization lock

Some managed setups never deploy an organization lock, and the CLI supports that permanently. An absent organization lock leaves that layer unlocked. Normal online runs, including `sync --check`, resolve the organization's explicit Marketplace requests into a verified lock that exists only in memory for that run, and never write `/etc`. Offline runs and `lock --check` need a deployed organization lock only to resolve the organization's Marketplace requests. Organization Git sources work without one. A present organization lock is exact: the CLI uses only what it records and stops with an error when it no longer matches the organization file.

Only `lock --scope organization`, run by an administrator who can write `/etc/agent-marketplace`, creates or updates the organization lock. The [configuration layers reference](./configuration-layers.md#organization-trust-rules) lists the ownership and permission rules a deployed organization lock must meet.

## Format versions

| `apiVersion` | Read | Written | Contents |
|---|---|---|---|
| `agent-marketplace-lock/v2` | yes | yes, for every writable layer | Git sources and Marketplace resources |
| `agent-marketplace-lock/v1` | yes | no | Git sources only |

v2 is the only format the CLI writes, including for layers that use only Git sources. When the CLI reads a v1 lock, it logs a `lock.version-deprecated` warning, and the next command that reconciles a writable lock rewrites it as v2. A read-only organization v1 lock stays v1 and keeps working. A v1 lock cannot contain Marketplace records.

::: warning
Agent Marketplace v4.0.9 and newer read every v2 lock. v4.0.8 rejects a v2 lock that holds only Git sources with `lock.invalid-document`. Every machine and CI job that reads a committed lock needs v4.0.9 or newer.
:::

## Document structure

A lock is one YAML document with these top-level fields. Unknown fields are rejected.

| Field | Description |
|---|---|
| `apiVersion` | `agent-marketplace-lock/v2` or `agent-marketplace-lock/v1`. |
| `scope` | The layer that owns the lock: `organization`, `user`, `repo`, or `local`. It must match the lock's location. |
| `registries` | One entry per Git repository and ref. |
| `marketplaces` | One entry per hosted Marketplace endpoint. v2 only. |

The CLI sorts every list in a fixed order when it writes, so an unchanged resolution produces an identical file.

## Git records

This lock was produced by `sync` for a repository configuration with one Git source at tag `v1.0.0`:

```yaml
apiVersion: agent-marketplace-lock/v2
scope: repo
registries:
  - url: https://github.com/example-org/agent-resources
    ref: v1.0.0
    commit: sha1:21b20eaa4ac27398c381c463a48b1b982de2b56c
    roots:
      - path: .
        source_harness: claude
        revision_schema: v4
        revision_digest: sha256:3ebe5d1a8489f21a6ddd5dd2624e5666f4d74677ce742b4ee14cce39c2e07ad1
        resources:
          - kind: command
            name: demo-cmd
            content_digest: sha256:c149c8dc1ae4fbc01e39f0d77bc69dae12fe82cad7fc81d622b89e974894b990
            archive_digest: sha256:c149c8dc1ae4fbc01e39f0d77bc69dae12fe82cad7fc81d622b89e974894b990
          - kind: mcpserver
            name: demo-mcp
            content_digest: sha256:93d2d6ba69c2bc95493cbb0e3b607adf784aaed0b547a3c4f6e40af5fd76795d
            archive_digest: sha256:93d2d6ba69c2bc95493cbb0e3b607adf784aaed0b547a3c4f6e40af5fd76795d
          - kind: skill
            name: demo-skill
            content_digest: sha256:c325a97c08646b39647a18e67ca6d9cec81cf06d849968ae16a982757c9521df
            archive_digest: sha256:c325a97c08646b39647a18e67ca6d9cec81cf06d849968ae16a982757c9521df
```

Each entry under `registries` has these fields:

| Field | Description |
|---|---|
| `url` | Normalized Git origin. A trailing `.git` is removed. |
| `ref` | The ref declared in the configuration. |
| `commit` | The exact commit the ref resolved to, prefixed with its hash algorithm: `sha1:` or `sha256:`. |
| `roots` | One entry per source path in that repository. |

Each root has these fields:

| Field | Description |
|---|---|
| `path` | Source path inside the repository, relative to its root. `.` is the repository root. |
| `source_harness` | Harness layout the source is written in. |
| `revision_schema` | Version of the rules used to build the root's inventory, `v1` to `v4`. `v4` is current. A lock with an unknown version fails with `lock.unsupported-version`. |
| `revision_digest` | Digest of the root's resource inventory. |
| `resources` | Every resource found under the root, including resources the configuration does not select. |

Each resource has these fields:

| Field | Description |
|---|---|
| `kind` | `skill`, `command`, `subagent`, `hook`, or `mcpserver`. Locks spell MCP servers `mcpserver`. |
| `name` | Resource name. |
| `content_digest` | Digest of the resource content. |
| `archive_digest` | Digest of the resource archive. |
| `support_files` | Digests of extra files that belong to the resource, keyed by path. Present only when there are any. |

The lock stores no local cache path. Before it fetches content, the CLI checks the recorded inventory against the source at the locked commit.

## Marketplace records

A `marketplace` source records one entry under `marketplaces` for each endpoint:

| Field | Description |
|---|---|
| `url` | Normalized Marketplace endpoint URL. |
| `instance_id` | Identity that the Marketplace reported and the CLI verified. |
| `registry_bindings` | Registry names used in locators, each with its immutable `registry_id`. |
| `resources` | One record per locked resource or collection. |

Each resource record has these fields:

| Field | When present | Description |
|---|---|---|
| `registry_name`, `registry_id` | always | The registry that serves the resource. The pair must match a registry binding. |
| `kind` | always | `skill`, `command`, `subagent`, `hook`, `mcpserver`, or `collection`. |
| `name` | always | Resource name in that registry. |
| `revision` | always | Exact resource revision. |
| `content_digest` | always | Digest of the resource content. |
| `archive_digest` | resources other than collections | Digest of the compiled archive. |
| `collection_manifest_digest` | collections | Digest of the collection manifest. Equal to `content_digest`. |
| `acquisition` | always | `mirror` when the CLI downloads the archive from the Marketplace, or `index-only` when it fetches the content from the source Git repository. Collections always use `mirror`. |
| `registry_commit` | `index-only` | Source Git commit, in lowercase. |
| `registry_manifest_digest` | `index-only` | Digest of the source registry manifest. |
| `source_path` | `index-only` | Path of the resource inside the source repository. |
| `selected_by` | resources selected through a collection | Each collection (`registry_id`, `name`, `revision`) that selected this resource. Every entry must point to a collection locked in the same file. |

The CLI installs a Marketplace resource only when the requested endpoint, registry, resource, and revision match a record in the owning layer's lock.

## Commands that read and write locks

| Command | Effect on locks |
|---|---|
| `lock` | Creates missing locks and updates stale ones as little as possible. Applies to the user, repository, and personal-local layers unless `--scope` narrows it. |
| `lock --check` | Reports changes `lock` would make, without writing. Also checks the organization lock. |
| `lock --upgrade` | Resolves the selected Git sources again at their current refs. Narrow it with resource names, `--registry`, `--ref`, `--path`, or `--kind`. |
| `lock --scope organization` | Creates or updates the organization lock. Requires write access to `/etc/agent-marketplace`. |
| `sync` | Installs from the locked commits and updates writable locks that no longer match the configuration. |
| `sync --locked` | Requires every lock to be current and never writes configuration or locks. |
| `sync --check` | Reports lock and output drift without writing, and exits `1` if anything is out of date. |
| `update` | Advances Git locks to the current commits of their refs, then syncs. `--bump` first moves eligible refs in legacy v2/v3 `registries:` entries to newer matching tags; it does not yet rewrite v4 `sources:`. |

A current lock does not move when the remote branch or tag moves. Use `lock --upgrade` or `update` to pick up new commits.

## Errors

Invalid lock files fail with a `lock.invalid` error that wraps one of these codes:

| Code | Meaning |
|---|---|
| `lock.malformed` | The file is not valid YAML. |
| `lock.conflicted` | The file contains Git merge conflict markers. |
| `lock.multiple-documents` | The file has more than one YAML document. |
| `lock.unknown-field`, `lock.duplicate-field` | A field is unknown or repeated. |
| `lock.alias-forbidden`, `lock.merge-forbidden` | The file uses a YAML alias or merge key. |
| `lock.unsupported-version` | Unknown `apiVersion` or `revision_schema`. |
| `lock.invalid-scope`, `lock.scope-mismatch` | `scope` is not a known layer, or does not match the lock's location. |
| `lock.invalid-document` | A record breaks a structural rule, such as a Marketplace resource without a matching registry binding. |
| `lock.identity-conflict` | Two records for the same source, resource, or support file disagree. |
| `lock.git-oid-invalid`, `lock.git-oid-unsupported` | A `commit` value is malformed or uses an unsupported hash algorithm. |
| `lock.digest-invalid` | A digest is malformed. |
| `lock.git-url-invalid` | A Git `url` cannot be normalized. |
| `lock.path-invalid` | A path is empty, absolute, contains `..` or a backslash, or is otherwise not portable. |

## Do not edit by hand

Lockfiles are generated. A hand edit skips the checks that tie a lock to its source inventory and to the Marketplace, and is not supported. Change the configuration and run `lock`, `sync`, or `update` instead.

## Related documentation

- [Repository configuration reference](./configuration.md)
- [How to keep resources reproducible](../how-to/keep-resources-reproducible.md)
- [Reproducibility and lockfiles](../explanation/reproducibility-and-lockfiles.md)
