# Configuration layers reference

## Layer order

Layers merge from lowest to highest precedence. A higher layer overrides a lower layer where the rules allow it.

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

A registry entry's own `harnesses` list intersects with the effective list; it cannot add a target that was not already selected.

### Allowlists

- `allowed_registries` can be declared by organization, user, or repository layers. `LocalConfig` cannot declare an allowlist.
- `allowed_marketplaces` can be declared by organization, user, or repository layers. It stops at repository; `LocalConfig` cannot declare an allowlist.
- Lower layers cannot relax an allowlist selected by a higher layer.

### Selectors

- Top-level `skills`, `commands`, `subagents`, `hooks`, `mcp`, and `collections` provide defaults for registry entries.
- A registry entry's own selector replaces the inherited default.
- `exclude_all` remains true once set by any contributor.

### Telemetry

The `telemetry` block is eligible in the organization and user layers, including fragments included by those layers. Repository, local, and their included fragments cannot declare telemetry. Included fragments can contain only the fields that their containing layer allows.

### `include`

Every layer supports local-file `include`. Included fragments merge below the file that includes them. The same file contributes only at its first position. Glob patterns and remote URLs are not supported.

## Layer-specific notes

- `apiVersion` is required for organization, user, and repository roots. It may be omitted in local roots and included fragments.
- Organization includes require root-owned, non-symlink files and directories with no group or other write permission.
- The organization lock is intentionally absent in some managed setups; an absent lock leaves that layer mutable, while a present lock is exact and fail-closed.

## Related documentation

- [Repository configuration reference](./configuration.md)
- [Scopes, selection, and precedence](../explanation/scopes-selection-and-precedence.md)
