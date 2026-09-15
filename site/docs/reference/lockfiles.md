# Lockfiles reference

This page describes the generated lockfiles, their location, and their role in reproducible sync.

## Layer-owned lockfiles

| Layer | Configuration | Generated lock |
|---|---|---|
| Organization | `/etc/agent-marketplace/config.yaml` | `/etc/agent-marketplace/lock.yaml` |
| User | `~/.agents/config.yaml` | `~/.agents/lock.yaml` |
| Repository | `<repo>/.agents/config.yaml` | `<repo>/.agents/lock.yaml` |
| Repository personal | `<repo>/.agents/config.local.yaml` | `<repo>/.agents/lock.local.yaml` |

## Content

Each lockfile is a YAML document with `apiVersion: agent-marketplace-lock/v1`, a `scope`, and a list of `registries`. Each registry entry records:

| Field | Description |
|---|---|
| `url` | Canonical Git origin. |
| `ref` | Declared ref. |
| `commit` | Exact resolved commit. |
| `roots` | One entry per configured root or discovered ingestion path. |

Each root records:

| Field | Description |
|---|---|
| `path` | Root path relative to the registry. |
| `source_harness` | Source harness used for compilation. |
| `revision_schema` | Revision schema for source manifest verification. |
| `revision_digest` | SHA-256 digest of the source manifest. |
| `resources` | Exact resources and their digests. |

Each resource records:

| Field | Description |
|---|---|
| `kind` | Resource kind (`skill`, `command`, `subagent`, `hook`, `mcp`, `collection`). |
| `name` | Resource name. |
| `content_digest` | SHA-256 digest of the primary content. |
| `archive_digest` | SHA-256 digest of the compiled archive. |
| `support_files` | Map of support-file paths to SHA-256 digests. |

No local snapshot path is stored. `agent-marketplace` reconciles the lock against the source manifest before exact acquisition.

## Behavior

- `agent-marketplace lock` creates or minimally reconciles generated Git locks.
- `agent-marketplace lock --check` reports required changes without writing.
- `agent-marketplace lock --upgrade` refreshes selected locks to the current ref deliberately.
- `agent-marketplace update` advances Git locks, optionally rewriting eligible refs first.
- `agent-marketplace sync` reconciles writable stale locks unless `--locked` or `--check` is set.
- Plain local-directory registries are live and do not appear in these locks.
- Hosted Marketplace resources use exact pins in consumer configuration rather than Git lockfile entries.
- A healthy, compatible lock does not silently advance when the remote `main` moves. Use `lock --upgrade` or `update` to move it deliberately.

## Lock modes

- Normal sync retains compatible exact commits and minimally reconciles stale writable locks.
- `sync --locked` requires fresh locks and forbids config or lock writes.
- `sync --check` is read-only and exits `1` if anything is out of sync.
- An absent organization lock leaves that layer intentionally unlocked.

## Do not edit by hand

The lockfile is generated. Manual edits bypass source manifest verification and are not supported.

## Related documentation

- [How to keep resources reproducible](../how-to/keep-resources-reproducible.md)
- [Reproducibility and lockfiles](../explanation/reproducibility-and-lockfiles.md)
