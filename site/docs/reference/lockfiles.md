# Lockfiles reference

## Layer-owned lockfiles

| Layer | Configuration | Generated lock |
|---|---|---|
| Organization | `/etc/agent-marketplace/config.yaml` | `/etc/agent-marketplace/lock.yaml` |
| User | `~/.agents/config.yaml` | `~/.agents/lock.yaml` |
| Repository | `<repo>/.agents/config.yaml` | `<repo>/.agents/lock.yaml` |
| Repository personal | `<repo>/.agents/config.local.yaml` | `<repo>/.agents/lock.local.yaml` |

Each lock belongs to the configuration layer that declares the source. The repository lock is committed with `.agents/config.yaml`; the personal-local lock is ignored. An absent organization lock intentionally leaves that external layer mutable. Normal online non-check runs resolve explicit organization Marketplace requests through a verified in-memory lock without writing `/etc`; offline and check-only runs require a deployed lock. A present organization lock is exact and fail-closed.

## Git lock records

A Git-only lock is a YAML document with `apiVersion: agent-marketplace-lock/v1`, a `scope`, and a list of `registries`. Each registry entry records:

| Field | Description |
|---|---|
| `url` | Canonical Git origin. |
| `ref` | Declared source ref. |
| `commit` | Exact resolved commit. |
| `roots` | One entry per configured source root. |

Each root records its relative path, source harness, source-manifest revision, and exact resources with their digests. No local snapshot path is stored; Agent Marketplace reconciles the lock against the source manifest before exact acquisition.

## Marketplace lock records

A v4 `marketplace` source uses a lock with `apiVersion: agent-marketplace-lock/v2` or `agent-marketplace-lock/v3`. The lock records verified endpoint identity, registry bindings, exact resource revisions, content digests, acquisition mode, and collection selection edges. The consumer resolves an explicit v4 request only when its endpoint, registry, resource, and revision match the lock evidence.

Marketplace lock records include:

| Field | Description |
|---|---|
| `url` | Canonical Marketplace endpoint. |
| `instance_id` | Verified Marketplace identity. |
| `registry_bindings` | Marketplace registry names and immutable IDs. |
| `resources` | Exact resource or collection records. |
| `selected_by` | Collection revisions that retain a resource. |

Lock-v2 mirror records carry the exact legacy `archive_digest`. Lock-v3 non-collection mirror records replace that field with exactly one tagged artifact selection:

```yaml
artifact:
  schema: v1
  archive_digest: sha256:<64 lowercase hex digits>
```

or:

```yaml
artifact:
  schema: v2
  artifact_digest: sha256:<64 lowercase hex digits>
  blob_digest: sha256:<64 lowercase hex digits>
```

Collections retain `collection_manifest_digest` and have no artifact selection. Index-only records retain their source archive digest, exact source commit, source path, and source-manifest digest.

All exact selections are authoritative, including lock-v2 and lock-v3/v1 selections after server backfill. Missing exact bytes do not permit a schema substitution. A v2 integrity failure is terminal, never a reason to retry v1.

Writable online lock reconciliation can upgrade v1 to v2 without changing the selected resource revision. It verifies the exact old archive, the complete candidate reference, and equivalence of payload, assets, licenses, and retained metadata, then commits the lock with configuration and old-lock generation checks. Missing source bytes or unsupported conversion leave the old pin in place with a diagnostic. Corruption or an equivalence mismatch fails the operation. Locked, check-only, offline, and read-only-layer operation never upgrade.

## Behavior

- `agent-marketplace lock` creates or minimally reconciles generated Git locks.
- `agent-marketplace lock --check` reports required changes without writing.
- `agent-marketplace lock --upgrade` refreshes selected Git locks to the current ref deliberately.
- `agent-marketplace update` advances Git locks, optionally rewriting eligible refs first.
- Plain directory sources are live and do not appear in Git locks.
- Hosted Marketplace source requests require matching verified Marketplace lock records.
- A healthy, compatible lock does not silently advance when a remote ref moves. Use `lock --upgrade` or `update` to move it deliberately.

## Lock modes

- Normal sync retains compatible exact commits and minimally reconciles stale writable locks.
- `sync --locked` requires fresh locks and forbids configuration or lock writes.
- `sync --check` is read-only and exits `1` if anything is out of sync.
- An absent organization lock leaves that layer intentionally unlocked. Explicit Marketplace requests use a transient verified lock during normal online non-check runs and never create the organization lock.

## Do not edit by hand

Lockfiles are generated. Manual edits bypass source-manifest and Marketplace verification and are not supported.

## Related documentation

- [Repository configuration reference](./configuration.md)
- [How to keep resources reproducible](../how-to/keep-resources-reproducible.md)
- [Reproducibility and lockfiles](../explanation/reproducibility-and-lockfiles.md)
