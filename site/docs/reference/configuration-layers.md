# Configuration layers reference

## Layer order

Layers merge from lowest to highest precedence. A higher layer overrides a lower layer where the rules allow it. Every root uses the v4 source/resource schema.

| Order | Layer | Configuration file | Generated lock | Purpose |
|---|---|---|---|---|
| 1 | Organization | `/etc/agent-marketplace/config.yaml` | `/etc/agent-marketplace/lock.yaml` | Managed machine policy and required resources |
| 2 | User | `~/.agents/config.yaml` | `~/.agents/lock.yaml` | Personal machine-wide targets and resources |
| 3 | Repository | `<repo>/.agents/config.yaml` | `<repo>/.agents/lock.yaml` | Shared project targets and resources |
| 4 | Repository personal | `<repo>/.agents/config.local.yaml` | `<repo>/.agents/lock.local.yaml` | Gitignored choices for one checkout |

## Authority rules

### Target harnesses

The effective target list is resolved in project-first order:

1. Repository committed `harnesses`
2. User personal `harnesses`
3. Organization `harnesses`

A resource group's `harnesses` list intersects with the effective list; it cannot add a target that was not already selected.

### Allowlists

- `allowed_registries` can be declared by organization, user, or repository layers. The personal-local layer cannot declare an allowlist.
- `allowed_marketplaces` can be declared by organization, user, or repository layers. The personal-local layer cannot declare an allowlist.
- Lower layers cannot relax an allowlist selected by a higher layer.

### Source and resource groups

- A v4 root declares named `sources` and `resources` maps.
- Every resource group key must identify a source in the same root or include tree.
- A resource group's `include`, `exclude`, `tags`, `all`, `required`, and `harnesses` fields are composed in include order.
- An explicit empty `tags: []` or `harnesses: []` is an override, not an omitted field.
- `required: true` protects selected resources but does not select resources by itself.

### Telemetry

The `telemetry` block is eligible in the organization and user layers, including fragments included by those layers. Repository, local, and their included fragments cannot declare telemetry. Included fragments can contain only the fields that their containing layer allows.

### `include`

Every layer supports local-file `include`. Included fragments merge below the file that includes them. Shared fragments expand once per include branch; only an ancestor repeated on the current branch is a cycle. Glob patterns, remote URLs, YAML aliases, and YAML merge keys are not supported.

## Layer-specific notes

- Every v4 root, including `config.local.yaml`, declares `apiVersion: agent-marketplace/v4`.
- An included fragment may omit `apiVersion` and inherits v4 from its root.
- Organization includes require root-owned, non-symlink files and directories with no group or other write permission.
- The organization lock is intentionally absent in some managed setups; an absent lock leaves that layer mutable, while a present lock is exact and fail-closed.

## Related documentation

- [Repository configuration reference](./configuration.md)
- [Scopes, selection, and precedence](../explanation/scopes-selection-and-precedence.md)
