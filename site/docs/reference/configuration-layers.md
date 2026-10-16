# Configuration layers reference

The CLI reads up to four configuration files on every run and combines them. Each file is a layer. This page lists the layers, where each file lives, which fields each may set, and how layers that disagree are resolved. For the fields themselves, see the [repository configuration reference](./configuration.md).

## Layer order

Layers merge from lowest to highest precedence. A higher layer overrides a lower layer where the rules below allow it. Every existing root in all four layers, including organization and personal-local, must declare exactly `apiVersion: agent-marketplace/v4`. Included fragments may omit the field or declare exact v4; a root cannot inherit its version. Unsupported input refuses the operation rather than being skipped. See [schema versions](./configuration.md#schema-versions) for diagnostics and [pre-upgrade conversion](../how-to/migrate-configuration.md) for older installations.

| Order | Layer | Configuration file | Generated lock | Purpose |
|---|---|---|---|---|
| 1 | Organization | `/etc/agent-marketplace/config.yaml` | `/etc/agent-marketplace/lock.yaml` | Managed machine policy and required resources |
| 2 | User | `~/.agents/config.yaml` | `~/.agents/lock.yaml` | Personal machine-wide targets and resources |
| 3 | Repository | `<repo>/.agents/config.yaml` | `<repo>/.agents/lock.yaml` | Shared project targets and resources |
| 4 | Repository personal | `<repo>/.agents/config.local.yaml` | `<repo>/.agents/lock.local.yaml` | Choices for one checkout, kept out of Git |

Each lock sits next to its configuration file and records only the sources that file declares. See the [lockfiles reference](./lockfiles.md).

`install` and `remove` write to the user, repository, or personal-local file, chosen with `--scope user`, `--scope repo`, or `--scope local`. Consumer commands never edit the organization file. A system administrator deploys it, usually with a configuration management tool.

## Fields each layer may set

Every layer may declare `sources`, `resources`, and `include`. The other top-level fields are limited by layer. A field in the wrong layer fails the whole layer with `v4.layer-field`, and the same rule applies to fragments that the layer includes.

| Field | Organization | User | Repository | Repository personal |
|---|---|---|---|---|
| `harnesses` | yes | yes | yes | no |
| `allowed_registries`, `allowed_marketplaces` | yes | yes | yes | no |
| `backup` | yes | yes | no | no |
| `logging` | accepted, not used | yes | no | no |
| `update_check` | yes | yes | no | no |
| `telemetry` | yes | yes | no | no |
| `materialization` | no | yes | yes | yes |
| Exclusion-only Marketplace source | no | yes | no | no |

An exclusion-only Marketplace source is a Marketplace resource group that contains only `exclude`. It lets a user hide optional resources that the organization file selects from the same endpoint. See [Hosted Marketplaces](./configuration.md#hosted-marketplaces).

The organization file accepts a `logging` block, but the CLI configures logging from the user file only. See the [logging reference](./logging.md).

## How layers combine

### Target harnesses

The CLI picks one top-level target list, project first:

1. A non-empty `harnesses` list in the repository file.
2. Otherwise, a non-empty `harnesses` list in the user file.
3. Otherwise, the organization `harnesses` list.

Lists are not combined across layers. A resource group's `harnesses` list intersects with this target list, so it can narrow the targets for that group but cannot add a target that is not already selected.

### Allowlists

`allowed_registries` and `allowed_marketplaces` are checked in the order organization, user, repository, and the first layer that declares the field controls it. Within that layer, the first declaration in include order wins. A later layer or fragment cannot relax or replace it. The personal-local file cannot declare either field.

### Sources and resource groups

- Every layer declares its own `sources` and `resources` maps.
- Every resource group key must name a source declared in the same file or its include tree. A layer cannot refer to a source declared by another layer.
- Resources selected by every layer are installed together. Name collisions between layers are resolved as described in [Name collisions](./configuration.md#name-collisions).
- `required: true` protects a group's selections from exclusions and from `remove`, but it does not select anything by itself.

### Backup, logging, and telemetry

These fields control machine-wide behavior, so repository files cannot change them.

- `backup` merges field by field from the organization file and then the user file. A value set in the user file wins over the organization value for the same field.
- `logging` comes from the user file only.
- `update_check` is read from the organization and user files. A value set in the user file wins over the organization value.
- `telemetry` is read from the organization and user files. See the [telemetry reference](./telemetry.md) for how the two combine.

### Materialization

`materialization` decides whether generated skills, commands, and subagents are [symlinks](../glossary.md#symlink) or regular files.

| File | Affects repository output | Affects machine output |
|---|---|---|
| User | yes, as the default | yes |
| Repository | yes | no |
| Repository personal | yes | no |
| Organization | not allowed | not allowed |

For repository output, the repository personal file wins over the repository file, which wins over the user file. Machine output uses only the user file.

## `include`

Every layer supports local-file `include`. Included fragments merge below the file that includes them: in a list, the first fragment merges first, each later fragment merges over the earlier ones, and the including file merges last. Fields keep their own merge rules, which the [repository configuration reference](./configuration.md#include) lists.

A fragment shared by two branches of the include tree is loaded once for each branch. Only a file that includes one of its own ancestors is a cycle (`v4.include-cycle`). Glob patterns, remote URLs, YAML aliases, and YAML merge keys are not supported. An included fragment may omit `apiVersion` and inherits v4 from its root.

Included fragments can contain only the fields that their layer allows. A fragment included by the repository file cannot set `telemetry`, for example.

### Organization trust rules

The organization file applies to every user on the machine, so the CLI checks file ownership and Unix permissions to confirm that only the system administrator can change it:

- `/etc/agent-marketplace/config.yaml` must be a regular file, not a symlink, owned by root, and not writable by group or others.
- The `/etc/agent-marketplace` directory must be owned by root, not a symlink, and not writable by group or others.
- Every included file must meet the same file rules. Every directory above an included file, up to the filesystem root, must meet the same directory rules. The CLI checks each path as written, before it resolves symlinks or `..` segments.
- The organization lock is optional: an absent `/etc/agent-marketplace/lock.yaml` leaves the layer unlocked (see [Organization lock](#organization-lock)). When a lock is deployed, it must meet the same file rules and declare `scope: organization`; a malformed, wrong-scope, or stale deployed lock is rejected before its remote resources are used.

A file or directory that breaks a rule fails the organization layer with an `organization.trust.*` diagnostic, such as `organization.trust.include.writable` or `organization.trust.ancestor.symlink`.

System symlink aliases do not get an exception. On macOS, `/etc`, `/var`, and `/tmp` are symlinks, so organization include paths must use the physical location, such as `/private/etc/agent-marketplace/profiles/company.yaml`.

User, repository, and personal-local files and their includes have no ownership or permission requirements.

## Organization lock

Some managed setups never deploy an organization lock. The CLI supports that permanently:

- When `/etc/agent-marketplace/lock.yaml` is absent, the organization layer stays unlocked. Online runs, including `sync --check`, resolve the organization's Marketplace requests into a verified lock held in memory for that run and never write `/etc`.
- When the lock is present, it is exact. The CLI installs only what it records, and it stops with an error, without touching the lock, when the lock no longer matches the organization file. The exception is `lock --scope organization` itself: it reconciles a mismatched lock, re-resolving only the entries whose source or ref changed and keeping the others at their locked commits (or moving every entry under `--upgrade`), so the administrator never has to delete the lock to relock it.
- `lock` and `lock --check` without `--scope` cover the user, repository, and personal-local locks, and `lock --check` also checks the organization lock. Only `lock --scope organization`, run by an administrator who can write `/etc/agent-marketplace`, creates or updates the organization lock.

## Related documentation

- [Repository configuration reference](./configuration.md)
- [Lockfiles reference](./lockfiles.md)
- [Scopes, selection, and precedence](../explanation/scopes-selection-and-precedence.md)
