# Why

Canonical Git URLs that end in `.git` lose their distinction from the same repository with one fewer suffix when a normalized lock is encoded or consumed again. Preserve this exceptional identity across existing v1/v2 lock lifecycles without changing wire fields, versions, or normal lock bytes.

# What Changes

- Preserve the input spelling needed to distinguish canonical identities ending in `.git` through v1/v2 decode, normalization, encode, and later lock operations.
- Keep ordinary Git URL output, existing YAML goldens, and acquisition-lock wire schemas unchanged.
- Carry the identity consistently through command acquisition, targeted upgrades, cache reachability, and configured fetch fallback.

# Capabilities

## Modified Capabilities

- `acquisition-lock-codec`: preserve repeated-suffix Git identities across existing lock round trips and lifecycle operations.
- `cache-garbage-collection`: retain exact Git cache objects and snapshots reachable from those identities.

# Impact

Changes are limited to `internal/lockfile`, `cmd/agent-marketplace`, and `internal/cache`. No lock field, API version, dependency, or external service changes.
