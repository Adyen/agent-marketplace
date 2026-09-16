# How to revoke a resource

Remove a resource or a specific revision from a hosted Marketplace.

## Prerequisites

- Operator access to the Marketplace configuration.
- The Marketplace `instance.id`.
- The registry ID, resource kind, and name to revoke.

## 1. Add a revocation

Revocations are string lists in the Marketplace operator configuration. They are evaluated from broad to narrow.

Revoke every resource from a registry:

```yaml
revocations:
  registry_ids:
    - untrusted-source
```

Revoke all revisions of a resource by coordinate:

```yaml
revocations:
  coordinates:
    - team-marketplace/shared-agents/skill/review
```

Revoke one exact revision:

```yaml
revocations:
  revisions:
    - team-marketplace/shared-agents/skill/review@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

## 2. Apply the configuration

Run `reindex` so the Marketplace reloads the configuration and applies revocations:

```bash
agent-marketplace serve reindex shared-agents --config /app/marketplace.yaml
```

## 3. Verify the revocation state

Request the resource and confirm it is reported as revoked:

```bash
curl -fs \
  "https://marketplace.example.com/api/v1/resources/shared-agents/skill/review" | head
```

A revoked revision response includes a `revocation` field with `kind`, `target_id`, `reason`, and `applied_at`. A `coordinates` target revokes every revision of that resource; a `revisions` target revokes only that digest.

## Boundaries

- Revocations apply before ingestion. A `revisions` target affects only one digest. A `coordinates` target affects every revision of that resource, including newer ones.
- Consumers can use `sync --allow-revoked <marketplace/registry/kind/name@sha256:...>` to run once with a revoked pin, but cannot make `sync --check` healthy for it.

## Related documentation

- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
- [How to reindex and monitor a Marketplace](./reindex-and-monitor.md)
