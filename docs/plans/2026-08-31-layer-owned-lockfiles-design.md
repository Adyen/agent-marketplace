# Layer-owned Git lockfiles design

**Status:** Implemented Git foundation

## Scope

This design makes remote Git registries reproducible without changing configuration ownership or selection precedence. It covers layer-owned lockfiles, exact Git commits and snapshots, source revision manifests, config-plus-lock transactions, command behavior, status, garbage collection, and the TUI dependency lifecycle.

It does not define lock records or acquisition adapters for a hosted Marketplace service. Plain filesystem directory registries also remain deliberately live and unlocked.

## Ownership and paths

A lock belongs to the same layer as the requirement that produced it:

| Configuration | Lock | Ownership |
|---|---|---|
| `/etc/agent-marketplace/config.yaml` | `/etc/agent-marketplace/lock.yaml` | externally managed organization |
| `~/.agents/config.yaml` | `~/.agents/lock.yaml` | user |
| `<repo>/.agents/config.yaml` | `<repo>/.agents/lock.yaml` | repository |
| `<repo>/.agents/config.local.yaml` | `<repo>/.agents/lock.local.yaml` | personal-local |

The repository lock is committed. The personal-local lock is ignored beside `config.local.yaml`. Consumer commands do not create an absent organization lock. If an organization lock exists, config and lock form one trust unit: both must pass the regular-file, ownership, permissions, ancestor, and symlink checks before the lock is execution input.

## Requirements and records

Configuration retains human-authored Git intent:

```yaml
registries:
  - url: https://git.example.com/agents/resources.git
    ref: main
    path: skills
    source_harness: claude
```

The owning lock records generated resolution state:

```yaml
apiVersion: agent-marketplace-lock/v1
scope: repo
registries:
  - url: https://git.example.com/agents/resources
    ref: main
    commit: sha1:0123456789abcdef0123456789abcdef01234567
    roots:
      - path: skills
        source_harness: claude
        revision_schema: v1
        revision_digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        resources:
          - kind: skill
            name: review
            content_digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
            archive_digest: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
```

Git records are grouped by secret-free canonical URL and declared ref because those requirements share one exact commit. Configured roots remain separate by normalized slash path and source layout. When configuration requests autodetection, the lock records the detected source harness.

Commit IDs are algorithm-qualified full OIDs. Schema v1 accepts SHA-1 and SHA-256. The acquisition boundary verifies the repository object format, checks out only the raw full OID, and confirms `HEAD` equals the requested OID.

## Git identity and local sources

The versioned Git canonicalizer is shared by planner matching, serialization, acquisition, and exact-cache keys. It accepts validated HTTP, HTTPS, SSH, SCP-style SSH, and hostless `file://` origins. It rejects credentials that would leak, controls, query strings, fragments, encoded separators, and traversal. Scheme and DNS host are normalized, default ports and one terminal `.git` are removed, while transport distinctions and SSH usernames that can affect repository identity remain distinct.

A hostless `file://` URL whose decoded path ends in `.git` is a lockable remote-style Git origin. A plain local directory is read live, is never copied into a lock, and does not become stale merely because files change.

## Source revision manifests

Every exact root has a deterministic manifest that separates:

- semantic resource content digests;
- canonical archive digests used for immutable extraction;
- support-file digests;
- the root-wide revision digest and schema.

Manifest construction validates all relative paths, rejects traversal and symlink escape, sorts map-derived output, and produces byte-stable results. Opening an exact snapshot verifies the commit and manifest rather than trusting a mutable ref cache. Missing, corrupt, or mismatched exact snapshots produce stable lock diagnostics and do not fall back to mutable content.

## Planning and transactions

The planner compares one layer's normalized requirements with only that layer's lock. Sync retains compatible records, acquires additions and invalidated roots, and removes records no longer required. Upgrade advances all or selected Git source occurrences. A resource-level target expands to its containing Git source because a commit is the atomic acquisition unit.

Config edits and lock reconciliation use a dependency transaction with validated paths, per-layer serialization, temporary staging, durable atomic publication, rollback, and startup recovery. Comment-preserving config bytes remain authoritative; generated lock bytes are deterministic. A failure before the commit point leaves both files unchanged, and a failure after publishing one side is recovered rather than accepted as a split dependency state.

## Command behavior

- `lock [--scope ...]` creates or reconciles writable layer locks.
- `lock --upgrade` advances all or selected Git requirements.
- `sync` uses locked exact snapshots and minimally reconciles writable stale locks.
- `sync --locked` refuses missing or stale required locks.
- `sync --check` and `lock --check` diagnose only from existing config, lock, exact-cache, and output bytes. They do not recover transactions, resolve mutable refs, fetch or populate a cold cache, create state roots, or reconcile dependencies and output; a required cold snapshot fails as unavailable.
- configured install discovery keeps every valid layer on its exact inventory and permits mutable bootstrap only for an entry whose own layer lock is missing.
- `update` advances Git locks, with `--bump` first changing eligible configured refs through the same transaction.
- read-only `list`, `status`, and deinit planning consume exact lock views without reconciliation.
- `status` reports each layer's exact Git lock state and distinguishes lock failures from materialization drift.
- `gc` takes the exclusive exact-cache namespace lease, then reloads the complete effective configuration and every layer lock before deriving the active immutable and legacy Git cache sets and removing only inactive entries. Any required config or lock preflight failure blocks deletion.
- the TUI invokes the same dependency lifecycle callbacks for mutating toggles and updates, carries the resulting lock-generation lease into sync, reloads only after the transaction and materialization complete, and surfaces failures without silently falling back to stale catalogs. Automatic load, reload, and refresh reads remain pinned to each valid layer lock; a mutable ref moving by itself does not replace the locked inventory.

## Determinism and safety

Lock YAML uses strict bounded decoding: aliases, merge keys, duplicate or unknown fields, multiple documents, excessive depth or nodes, and conflict markers are rejected. URLs, OIDs, digests, roots, resources, and support paths are validated before use. Output ordering is explicit and duplicate identities are rejected or deterministically deduplicated where the schema permits it.

Writers compare desired bytes before publishing, use same-directory temporary files and atomic rename, and preserve modes. When synchronization has exact Git consumers, it acquires and revalidates one shared exact-cache namespace lease before resolution, then holds it through catalog discovery and materialization; synchronization with no exact consumers takes no namespace lease. Read-only exact consumers propagate that lease through discovery without also taking the legacy mutable-cache lease, avoiding an inverted exact/legacy lock order. Status and the TUI retain their read-only exact lease until dry-run or materialization has finished consuming resolved snapshot paths. Check mode with exact consumers opens the existing lease read-only, without creating or repairing coordination state, and missing or unsafe coordination fails closed. Garbage collection takes the exclusive counterpart and reloads the complete effective configuration plus every layer lock under that lease, so it cannot delete a verified snapshot before its final consumer finishes or omit newly published reachability. Lock publication uses the shared lease across transaction commit so collection cannot race a newly active commit after its final reachability reload. All destructive paths fail closed. Lockfiles contain no credentials, cache paths, plain-directory paths, verification markers, or transient policy decisions.

## Verification

The implementation retains unit, golden, planner, benchmark, exact-cache, transaction, command, pipeline, TUI, and end-to-end lifecycle coverage. Tests use isolated homes, repositories, state directories, and local bare Git origins; no test depends on the developer machine or an external network service.
