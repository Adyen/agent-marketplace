# Collections reference

## Collection structure

A collection is a named bundle declared in a registry. The collection file lives at `collections/<name>/COLLECTION.yaml`.

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

## Member fields

The fields in `COLLECTION.yaml` are:

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Collection name. |
| `description` | string | no | Short description. |
| `version` | string | no | Display-only version metadata; matching is by `name`. |
| `resources` | list | no | General member entries. |
| `skills` | list | no | Legacy skill-only member entries. |
| `includes` | list | no | Nested collections to include. |

## `resources` item

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Resource name. |
| `kind` | string | no | `skill`, `command`, `subagent`, `hook`, `mcp`, or `collection`. Defaults to `skill` when omitted. |
| `registry` | string | no | Provider registry hint. |
| `path` | string | no | Provider path hint. |
| `ref` | string | no | Provider ref hint. |

## `skills` item (legacy)

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Skill name. |
| `registry` | string | no | Provider registry hint. |
| `path` | string | no | Provider path hint. |
| `ref` | string | no | Provider ref hint. |

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

Shared children are valid across sibling include branches. An ancestor repeated on the same include path is a cycle and is rejected.

## Consumer selection

A consumer selects a collection like any other resource:

```yaml
collections:
  include:
    - platform-onboarding
```

Collections expand into their members before resource resolution. The collection itself is not materialized.

## Removal

Removing a collection member records an `exclude` in the active scope while keeping the collection selected. Removing the collection name removes the collection selection itself.

## Related documentation

- [How to publish resource collections](../how-to/author/publish-resource-collections.md)
- [How to install resource bundles](../how-to/install-resource-bundles.md)
- [Registry layout reference](./registry-layout.md)
