# How to revoke a resource revision

Mark a resource, or one exact revision of it, as revoked in a hosted Marketplace so consumers can no longer install it. Revocation is a policy record: the immutable artifact bytes and revision history are retained, and existing consumer pins that reference the revoked digest start failing `sync --check`.

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
curl -fsS "https://marketplace.example.com/api/v1/resources/shared-agents/skill/review" -o revision.json
python3 -c 'import json,sys; r=json.load(open("revision.json")); print(r.get("revocation"))'
```

A revoked revision has a non-null `revocation` object with `kind`, `target_id`, `reason`, and `applied_at`. `None` means the latest revision is not revoked. Avoid `curl ... | head` for this check: without `set -o pipefail` the pipeline reports success even when `curl` fails.

## Boundaries

- Revocations apply before ingestion. A `revisions` target affects only one digest. A `coordinates` target affects every revision of that resource, including newer ones.
- Consumers can run one sync with `sync --allow-revoked <instance_id>/<registry>/<kind>/<name>@sha256:...` for a revoked pin, but that is execution-only and cannot make `sync --check` healthy.

## Related documentation

- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
- [How to reindex and monitor a Marketplace](./reindex-and-monitor.md)
