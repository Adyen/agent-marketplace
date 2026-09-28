# How to revoke a resource revision

Mark a resource, or one exact revision of it, as revoked in a hosted Marketplace so consumers can no longer install it. A revocation is a policy record. The Marketplace keeps the immutable artifact bytes and revision history, and consumer pins that reference a revoked digest start failing `sync --check`.

## Prerequisites

- Operator access to the Marketplace configuration and the ability to restart the API and worker processes.
- The Marketplace `instance.id`.
- The registry ID, resource kind, and name to revoke.

## 1. Add a revocation

Revocations are lists of strings under `revocations:` in the operator configuration. They work the same way in `file` and `database` management modes.

Revoke every resource from a registry:

```yaml
revocations:
  registry_ids:
    - untrusted-source
```

Revoke all revisions of a resource by coordinate (`<instance.id>/<registry ID>/<kind>/<name>`):

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

The configuration fails to load if a coordinate or revision names a different Marketplace than `instance.id`. When several declarations match one revision, the Marketplace reports the most specific: an exact revision, then a coordinate, then a registry.

## 2. Apply the configuration

Deploy the updated configuration to every API and worker process and restart them. With the Helm chart, a change to `config.content` rolls the pods automatically.

Each `serve` process makes the stored revocations match its configuration before it serves anything, and the API applies the declarations from its own configuration to every read. It does not reload the file while running.

::: warning
`/readyz` returns `503` on any API process whose configuration does not match the stored revocations. During a rollout, processes that still run the old configuration report not ready until they restart. For the same reason, do not run `serve reindex` with the new configuration while API processes still run the old one.
:::

## 3. Verify the revocation state

Request the resource and check its revocation:

```bash
curl -fsS "https://marketplace.example.com/api/v1/resources/shared-agents/skill/review" -o revision.json
python3 -c 'import json,sys; r=json.load(open("revision.json")); print(r.get("revocation"))'
```

A revoked revision has a `revocation` object with `kind` (`registry`, `coordinate`, or `revision`), `target_id`, `reason`, and `applied_at`, and its `current_availability.availability` is `revoked`. `None` means the latest revision is not revoked. With `management.mode: database`, the API answers a revoked resource with `410 Gone`, so `curl -f` fails instead of saving the file.

Avoid `curl ... | head` for this check: without `set -o pipefail`, the pipeline reports success even when `curl` fails.

## Boundaries

- A `revisions` entry affects one digest. A `coordinates` entry affects every revision of that resource, including future ones. A `registry_ids` entry affects every resource from that registry.
- Removing an entry from the configuration withdraws the revocation the next time a process reconciles it.
- Consumers can run a single sync against a revoked pin with `sync --allow-revoked <instance_id>/<registry>/<kind>/<name>@sha256:...`. The override applies only to that run, and `sync` rejects it together with `--check`, so the check stays unhealthy.

## Related documentation

- [Marketplace operator configuration reference](../../reference/marketplace-operator-configuration.md)
- [How to reindex and monitor a Marketplace](./reindex-and-monitor.md)
