# Collections reference

A [collection](../glossary.md#collection) is a named list of resources that a registry publishes so a consumer can select all of them with one entry. This page lists every field in `COLLECTION.yaml`, how members are found, and how selection and removal work. Registry authors and anyone debugging a collection that does not expand as expected will need it.

## Collection structure

Each collection lives in its own directory: `collections/<name>/COLLECTION.yaml`. The `collections/` directory sits at the registry root, or at the root of the directory a source's `path` points to. Each sub-bundle that the catalog discovers inside a registry can also have its own `collections/` directory.

```yaml
name: platform-onboarding
description: Common starter resources for platform teams
resources:
  - kind: skill
    name: frontend-design
  - kind: command
    name: release
    registry: https://github.com/example-org/shared-commands.git
    path: commands
```

Discovery rules:

- Only subdirectories of `collections/` are read. A flat file such as `collections/platform-onboarding.yaml` is skipped without a warning.
- A subdirectory without a `COLLECTION.yaml` file is skipped.
- A `COLLECTION.yaml` that is not valid YAML, has no `name`, or has a member with an unsupported `kind` is an error. `agent-marketplace list` exits with status 1 when it hits one.
- Consumers find a collection by its `name` field. The directory name is not used for matching.

## Member fields

The top-level fields in `COLLECTION.yaml` are:

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Collection name. Consumers select the collection by this value. |
| `description` | string | no | Short description shown in listings. |
| `version` | string | no | Display-only version metadata. Matching uses `name` only, because the source's ref already pins which version is in use. |
| `resources` | list | no | Member entries of any supported kind. |
| `skills` | list | no | Legacy member list. Each item has the same fields as a `resources` item except `kind`, and every item is a skill. Use `resources` in new collections. |
| `includes` | list | no | Other collections to nest inside this one. |

## `resources` item

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Resource name. Items with an empty name are ignored. |
| `kind` | string | no | `skill`, `command`, `subagent`, `hook`, or `mcp` (`mcpserver` is also accepted). Matching ignores case. Defaults to `skill` when omitted. Any other value, including `collection`, is an error: `list` and `sync` fail with `unsupported collection member kind`. |
| `registry` | string | no | URL of the registry that provides the resource. Defaults to the registry that contains the collection. |
| `path` | string | no | Directory inside the providing registry. Defaults to the collection's own source path when `registry` is omitted. |
| `ref` | string | no | Git ref in the providing registry. See the rules below for the default. |

How `registry` and `ref` combine:

| `registry` | `ref` | Result |
|---|---|---|
| omitted | omitted | The member comes from the same registry, ref, and path as the collection. |
| omitted | set | The member comes from the same registry at the given ref. |
| set | omitted | The consumer's configuration must already contain that registry. The member uses the ref configured there. Otherwise expansion fails with `references registry ... which has no configured ref and none was provided`. |
| set | set | The consumer's configuration must already contain that registry. Otherwise expansion fails with `is not present in any config layer; add it to your registry list first`. |

A collection cannot add a new registry to the consumer's configuration. If members come from another registry, tell consumers to configure that registry as a source first.

A member name that the providing registry does not contain produces a warning during `sync`, and `sync --strict` fails on it. `list` does not check members.

## Nested collections

A collection can include other collections with `includes`:

```yaml
name: onboarding
description: Onboarding collection with shared bundles
includes:
  - name: security
  - name: platform
    version: v1.2.0
```

Each `includes` item has these fields:

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Name of the collection to include. |
| `version` | string | no | Display-only. It does not affect which collection is found. |

An included collection can come from any registry in the consumer's configuration. If no configured registry has a collection with that name, expansion fails with `includes "<name>" which was not found in configured registries`.

The same collection can appear in several sibling branches, and its members are selected once. A collection that includes one of its own ancestors on the same include path is a cycle, and expansion fails with `collection cycle detected`.

## Consumer selection

A consumer selects a collection by adding `collection/<name>` to a resource group's `include` list:

```yaml
apiVersion: agent-marketplace/v4
sources:
  shared:
    git: https://github.com/example-org/shared-agents.git
    ref: main
resources:
  shared:
    include:
      - collection/platform-onboarding
```

`agent-marketplace install platform-onboarding --kind collection` writes the same entry. During resolution, the collection expands into its members before any other selection happens. The collection itself produces no files in the harness folders; only its members do. Excludes that the consumer configured for a providing registry still apply to the members that the collection selects there.

## Removal

Removing a collection member records an `exclude` in the active scope while keeping the collection selected. For example, `agent-marketplace remove frontend-design` adds `skill/frontend-design` to the group's `exclude` list and prints `Excluded frontend-design (provided by collection:platform-onboarding)`. When the collection sets `version`, the label reads `collection:<name>@<version>`. Removing the collection name removes the collection selection itself and prints `Removed collection <name>`.

## Related documentation

- [How to publish resource collections](../how-to/author/publish-resource-collections.md)
- [How to install resource bundles](../how-to/install-resource-bundles.md)
- [Registry layout reference](./registry-layout.md)
