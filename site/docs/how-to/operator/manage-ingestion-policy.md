# How to manage ingestion policy

Configure the license, hidden-content, and size policy that the worker applies when it indexes registries for a hosted Marketplace.

These steps apply with `management.mode: file` (the default). In `database` mode, the admin panel owns the license policy and `rejected_resources`, while `hidden_content` and `limits` still come from the operator configuration. See [How to enable and use the Marketplace admin panel](./enable-and-use-the-admin-panel.md).

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

If you omit `license_allowlist`, the worker uses these four licenses. An explicit empty list (`license_allowlist: []`) turns off license enforcement, so no resource is rejected or warned about on license grounds.

The worker reads a resource's license from its `license` or `metadata.license` field. When the resource declares none, it uses the registry's `license` field. A registry `license` that is not in the allowlist stops the configuration from loading.

## 2. Choose the undeclared license behavior

```yaml
policy:
  license_undeclared: reject
```

Accepted values:

- `reject` (default): block the resource.
- `warn`: keep it installable and record a warning finding.
- `allow`: accept it without recording a finding.

This setting has no effect when the allowlist is empty. Any other value stops the configuration from loading.

## 3. Choose the hidden-content behavior

```yaml
policy:
  hidden_content: warn
```

`warn` is the default. `reject` blocks resources that contain HTML comments or zero-width characters.

## 4. Choose what happens to rejected resources

```yaml
policy:
  rejected_resources: block
```

`block` (the default) withholds rejected revisions. `expose` serves them with the `operator-exposed` availability and keeps their reject assessment. Revocations and integrity failures stay blocked either way.

## 5. Set acquisition and resource limits

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

The values shown are the defaults. An omitted limit or a value of `0` uses the default, and a negative value stops the configuration from loading.

## 6. Override policy per registry

```yaml
registries:
  - id: internal-agents
    slug: internal-agents
    origin: https://github.com/example-org/internal-agents.git
    ref: main
    roots:
      - path: .
    policy_overrides:
      license_undeclared: warn
      limits:
        repository_bytes: 52428800
```

`policy_overrides` accepts `hidden_content`, `license_allowlist`, `license_undeclared`, `rejected_resources`, and a nested `limits` block. The worker ignores unknown keys. It also ignores a malformed value and keeps the global setting, except for `rejected_resources`, where an invalid value stops the configuration from loading. An empty `license_allowlist` override keeps the global allowlist.

The clone step always enforces the global `repository_bytes`, `file_count`, `file_bytes`, and `clone_duration`. An override can make those size limits stricter during ingestion but cannot raise them above the global values, and a `clone_duration` override has no effect.

## 7. Reindex to apply policy

```bash
agent-marketplace serve reindex internal-agents --config /app/marketplace.yaml
```

## License findings

When license enforcement is on, the worker records one of these finding codes and logs the decision with the same code:

- `marketplace.policy.license-not-allowed`: the declared license is not in the allowlist. The resource is rejected.
- `marketplace.policy.license-undeclared`: the resource declares no license. `license_undeclared` decides whether the resource is rejected, installable with a warning, or accepted (in which case no finding is recorded).

An artifact larger than `artifact_bytes` is rejected with `marketplace.policy.artifact-too-large`.

## Related documentation

- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
