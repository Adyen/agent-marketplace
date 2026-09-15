# How to manage ingestion policy

Configure license and content policy for a hosted Marketplace.

## Prerequisites

- A Marketplace operator configuration file.
- The SPDX identifiers of the licenses you want to allow.

## 1. Set the license allowlist

```yaml
policy:
  license_allowlist:
    - MIT
    - Apache-2.0
    - BSD-2-Clause
    - BSD-3-Clause
```

An empty list disables license enforcement entirely.

## 2. Choose the undeclared license behavior

```yaml
policy:
  license_undeclared: reject
```

Accepted values:

- `reject`: block the resource (default).
- `warn`: keep it installable and record a warning.
- `allow`: accept it without recording.

## 3. Set acquisition and resource limits

```yaml
policy:
  limits:
    repository_bytes: 104857600
    file_count: 1024
    file_bytes: 2097152
    artifact_bytes: 16777216
    clone_duration: 5m
    ingestion_duration: 2m
```

## 4. Override policy per registry

```yaml
registries:
  - id: internal-agents
    slug: internal-agents
    origin: https://github.com/example-org/internal-agents.git
    ref: main
    policy_overrides:
      license_undeclared: warn
      limits:
        repository_bytes: 52428800
```

Unknown override fields are ignored.

## 5. Reindex to apply policy

```bash
agent-marketplace serve reindex internal-agents --config /app/marketplace.yaml
```

## License outcomes

- `disallowed`: the declared license is not in the allowlist; the resource is rejected.
- `undeclared`: the resource has no license; the outcome is controlled by `license_undeclared`.

## Related documentation

- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
