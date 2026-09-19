# Lockfiles reference

## Layer-owned lockfiles

| Layer | Configuration | Generated lock |
|---|---|---|
| Organization | `/etc/agent-marketplace/config.yaml` | `/etc/agent-marketplace/lock.yaml` |
| User | `~/.agents/config.yaml` | `~/.agents/lock.yaml` |
| Repository | `<repo>/.agents/config.yaml` | `<repo>/.agents/lock.yaml` |
| Repository personal | `<repo>/.agents/config.local.yaml` | `<repo>/.agents/lock.local.yaml` |

Each lock belongs to the configuration layer that declares the source. The repository lock is committed with `.agents/config.yaml`; the personal-local lock is ignored. An absent organization lock intentionally leaves that external layer mutable. A present organization lock is exact and fail-closed.

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

A v4 `marketplace` source uses a lock with `apiVersion: agent-marketplace-lock/v2`. The lock records verified endpoint identity, registry bindings, exact resource revisions, content digests, acquisition mode, and collection selection edges. The consumer resolves an explicit v4 request only when its endpoint, registry, resource, and revision match the lock evidence.

Marketplace lock records include:

| Field | Description |
|---|---|
| `url` | Canonical Marketplace endpoint. |
| `instance_id` | Verified Marketplace identity. |
| `registry_bindings` | Marketplace registry names and immutable IDs. |
| `resources` | Exact resource or collection records. |
| `selected_by` | Collection revisions that retain a resource. |

Mirror records carry the compiled archive digest. Index-only records carry the exact source commit and source-manifest digest needed for the consumer to acquire the content from Git.

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
- An absent organization lock leaves that layer intentionally unlocked.

## Do not edit by hand

Lockfiles are generated. Manual edits bypass source-manifest and Marketplace verification and are not supported.

## Related documentation

- [Repository configuration reference](./configuration.md)
- [How to keep resources reproducible](../how-to/keep-resources-reproducible.md)
- [Reproducibility and lockfiles](../explanation/reproducibility-and-lockfiles.md)
