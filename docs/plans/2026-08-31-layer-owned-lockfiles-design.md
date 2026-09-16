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

The repository lock is committed. The personal-local lock is ignored beside `config.local.yaml`. An absent organization lock is an intentional unlocked authority: consumer commands may inspect or refresh those requirements through the mutable cache but do not create the lock. If an organization lock exists, config and lock form one exact, fail-closed trust unit: both must pass the regular-file, ownership, permissions, ancestor, and symlink checks before the lock is execution input, and an invalid present lock never falls back to mutable acquisition.

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
        revision_schema: v2
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

Every exact root has an interpreted resource inventory. `revision_schema` selects the complete supported source-generator contract, not just a JSON shape. `internal/lockfile/revision.go` is the authoritative supported-version registry used by lock decoding, source building, manifest encoding and verification. No independent CLI-version or HCC-version stamp is stored. The manifest's generator string is derived from that contract and must match it exactly.

The supported contracts are:

| Revision | Discovery and primary content | Support source set |
|---|---|---|
| `v1` (legacy, still supported) | HCC catalog ingestion for the recorded harness; fallback catalog discovery when loading fails; separately discovered collections; duplicate identities rejected; unsupported rules/unknown kinds and unbuildable resources omitted with warnings | Skill support paths reported by ingestion only; no hook support digests |
| `v2` (legacy, still supported) | Same discovery and primary-content rules as v1 | Skill support paths plus hook-adjacent non-directory entries other than `hooks.json`, using the same `catalog.HookSupportFiles` enumeration as materialization; no recursive hook-directory traversal or command-string dependency inference |
| `v3` (new acquisitions) | Same primary-content rules as v2; Windsurf native `hooks.json` takes precedence over legacy per-event directories, and distinct handlers receive unique names while the prior last-handler survivor retains its original selector | Same support-source rules as v2 |

For all supported versions, `content_digest` is SHA-256 of the readable primary source file's raw bytes, not a normalized semantic representation. Resources without a readable source file use JSON serialization of their IR (including collections). Each support digest is SHA-256 of its raw file bytes. `archive_digest` is a source-set digest, **not** a tar/zip hash: hash the primary payload followed by each support entry in lexical path order, appending a NUL, the relative slash path, another NUL, and its algorithm-qualified digest string. Collections have content digests only and are omitted from the lock's resource projection, but contribute to the root digest. File modes are authenticated by the exact Git tree, not these content digests.

The root digest is SHA-256 of compact JSON containing `schema_version`, the contract's `generator`, and the resources sorted by kind/name. Resource fields are ordered kind, name, content digest, optional archive digest, optional support map; support keys are sorted. HTML escaping is disabled and there is no trailing newline. `revision_digest` itself is excluded when hashing. These rules, resource identities, fallback serialization, omission policy and support enumeration are versioned together.

### Compatibility and migration lifecycle

Verification dispatches on the lock's recorded revision, never the current default. Building with an explicit v1 or v2 input regenerates the legacy inventory, including Windsurf's directory-first preference and last-handler survivor. Normal `lock`, `sync`, `sync --locked`, checks and read-only consumers retain valid v1/v2 records without migrating them. Newly acquired or explicitly upgraded roots use v3. This source-generator revision is distinct from the configuration `apiVersion` v3 compatibility alias. `lock --upgrade` / `update` opt into regeneration and also resolve the declared ref, so they may advance the commit; they are not metadata-only migrations. To require the same commit during regeneration, use an exact-OID requirement. Organization lock migration remains organization-owned.

Unknown or retired revisions fail with typed `lock.unsupported-version` before interpreting the snapshot; even upgrade does not silently reinterpret such a lock. Use a CLI that supports its recorded revision to inspect or regenerate it. A supported revision whose rebuilt inventory differs fails with `lock.manifest-mismatch`, not automatic relocking.

The independently authored `internal/sourcerevision/testdata/compatibility-v1.json` and `compatibility-v2.json` vectors pin all six supported inventory kinds, raw-file and collection-IR encoding, skill and hook support, and malformed-sibling omission across Claude and Factory inputs. They were calculated from authored payloads using independent SHA-256/JSON calculations, not production builder output. Tests verify both generation and verification against these bytes, and demonstrate that a hook-support edit affects v2 but not legacy v1. Existing version vectors must not be rewritten to bless changed semantics: changes to discovery, names, validation/omission, IR serialization or support rules require a new revision and new vectors. Preserve a compatibility implementation for supported old revisions; if that is no longer feasible, explicitly retire the revision in the registry and reject it with the typed unsupported diagnostic. Add fixtures for newly encountered compatibility cases.

This is an explicit support lifecycle, not a frozen duplicate of all HCC code or proof that fixtures cover every possible source. HCC is built into the CLI and shared implementation changes require compatibility review. Git commits pin source trees, recorded harnesses pin source-layout selection, and supported generator revisions check the interpreted inventory. Target compilation, target capability changes and byte-identical materialized output across CLI releases are **not** pinned.

Manifest construction validates relative paths, rejects traversal and support-file symlinks, sorts map-derived output, and produces byte-stable results. Opening an exact snapshot independently verifies the commit and every tracked file's bytes/mode before manifest verification. Missing, corrupt, or mismatched snapshots produce stable lock diagnostics and never fall back to mutable content.

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
- read-only `list`, `status`, and `deinit --dry-run` consume present locks as exact views without reconciliation; an absent organization lock instead permits mutable-cache inspection without creating a lock.
- user deinit refreshes an unlocked organization registry when needed to preserve its machine output and cache; an invalid present organization lock blocks teardown rather than downgrading to mutable acquisition.
- `status` reports each layer's exact Git lock state and distinguishes lock failures from materialization drift.
- `gc` takes the exclusive exact-cache namespace lease, then reloads the complete effective configuration and every layer lock before deriving the active immutable and legacy Git cache sets and removing only inactive entries. Any required config or lock preflight failure blocks deletion.
- the TUI invokes the same dependency lifecycle callbacks for mutating toggles and updates, carries the resulting lock-generation lease into sync, reloads only after the transaction and materialization complete, and surfaces failures without silently falling back to stale catalogs. Automatic load, reload, and refresh reads remain pinned to each present valid layer lock; only an intentionally absent organization lock uses mutable discovery, and a mutable ref moving by itself does not replace any locked inventory.

## Determinism and safety

Lock YAML uses strict bounded decoding: aliases, merge keys, duplicate or unknown fields, multiple documents, excessive depth or nodes, and conflict markers are rejected. URLs, OIDs, digests, roots, resources, and support paths are validated before use. Output ordering is explicit and duplicate identities are rejected or deterministically deduplicated where the schema permits it.

Writers compare desired bytes before publishing, use same-directory temporary files and atomic rename, and preserve modes. When synchronization has exact Git consumers, it acquires and revalidates one shared exact-cache namespace lease before resolution, then holds it through catalog discovery and materialization; synchronization with no exact consumers takes no namespace lease. Read-only exact consumers propagate that lease through discovery without also taking the legacy mutable-cache lease, avoiding an inverted exact/legacy lock order. List and status retain their read-only exact lease until the materialization dry run has finished, and user deinit retains any exact lease until removal materialization has consumed the organization-owned snapshot paths. The TUI copies each bounded detail source while the lease is held and retains the lease until its dry run finishes, so later detail rendering does not reopen an unleased snapshot. Check mode with exact consumers opens the existing lease read-only, without creating or repairing coordination state, and missing or unsafe coordination fails closed. Garbage collection takes the exclusive counterpart and reloads the complete effective configuration plus every layer lock under that lease, so it cannot delete a verified snapshot before its final consumer finishes or omit newly published reachability. Normal lock reconciliation acquires the shared lease before its first exact snapshot acquisition or verification and retains it through dependency publication; recovery publication likewise retains the shared lease through its transaction commit. Collection therefore cannot delete a newly acquired snapshot before its lock becomes reachable or race a newly active or recovered commit after its final reachability reload. All destructive paths fail closed. Lockfiles contain no credentials, cache paths, plain-directory paths, verification markers, or transient policy decisions.

## Verification

The implementation retains unit, golden, planner, benchmark, exact-cache, transaction, command, pipeline, TUI, and end-to-end lifecycle coverage. End-to-end subprocesses isolate home, XDG, Codex, organization-config, marketplace-policy, and executable-path environment settings; tests use temporary repositories and local bare Git origins, with no dependency on the developer machine or an external network service.
