# Layer-owned lockfiles

**Status:** Approved design, ready for implementation planning

**Date:** 2026-08-31

**Origin:** An approved brainstorming, domain-modeling, and codebase-design session on 2026-08-31

## Promotion boundary

The user approved this design section by section before this document was written. It authorizes implementation planning, not an incomplete compatibility mode in which exact Marketplace state is maintained in both config and lockfiles indefinitely.

## Objective

Make repeated synchronization reproducible without forcing users to manage immutable revisions by hand. Human-authored configuration declares acquisition intent. Generated lockfiles record exact resolutions and integrity data. Normal sync minimally reconciles requirements while preserving compatible locked resolutions; update deliberately seeks newer resolutions.

The intended user experience follows uv more closely than npm or Go:

- a missing or config-stale writable lock is reconciled automatically;
- an existing compatible resolution is retained even when a newer upstream revision exists;
- a strict mode proves that locks are current without rewriting them;
- explicit update advances all or selected resolutions;
- generated lock diffs clearly expose changed source and resource content.

Background comparison with Go modules, npm, and uv is in [the package-manager lockfile research note](./2026-08-31-lockfile-package-manager-research.md).

## Non-goals

- Resolving dependencies between resources; the only closure in v1 is collection expansion.
- Freezing Marketplace assessment or revocation policy.
- Making plain filesystem directory registries immutable.
- Persisting cache locations or cache verification markers.
- Supporting a second long-lived model in which Marketplace resolution is authoritative in both config and lockfiles.
- Defining a lockfile merge driver. Conflicted generated files are regenerated after config conflicts are resolved.
- Providing Windows-specific behavior.

## Domain model

A **requirement** is human-authored acquisition intent owned by one configuration layer. It may name a mutable Git ref or a Marketplace resource without fixing its current revision.

A **locked resolution** is the immutable source occurrence or resource revision selected for one requirement. Compatibility is determined from the requirement and lock record; publishing a newer revision does not make an existing resolution incompatible.

A **lockfile** is generated, scope-owned state that maps one layer's remote requirements to locked resolutions. Locks are composed with their owning config layers before the existing merge, selection, collision, and materialization logic runs.

A **source revision manifest** identifies the complete discovered resource inventory under one exact Git source root. It records semantic content, canonical archive, and support-file digests independently rather than pretending one digest has all three meanings.

## Lock ownership and paths

Each configuration authority owns an adjacent lock:

| Configuration | Lockfile | Ownership |
|---|---|---|
| `/etc/agent-marketplace/config.yaml` | `/etc/agent-marketplace/lock.yaml` | Externally managed organization |
| `~/.agents/config.yaml` | `~/.agents/lock.yaml` | User-personal |
| `<repo>/.agents/config.yaml` | `<repo>/.agents/lock.yaml` | Repo committed |
| `<repo/subdir>/.agents/config.local.yaml` | `<repo/subdir>/.agents/lock.local.yaml` | Repo personal |

The repo lock is committed. The local lock is gitignored beside `config.local.yaml`. User locks remain machine-local. An organization may deploy a lock beside its config; consumer commands never rewrite either implicitly.

When an organization lock exists, the organization config and lock form one trust unit. Before treating the lock as authoritative execution input, the loader requires both paths to be regular files owned by a trusted system owner, rejects group/other write permission, and validates every existing ancestor from `/etc/agent-marketplace` without following symlinks. A symlinked, wrongly owned, writable, non-regular, malformed, or stale organization lock is a hard trust error and never falls back to a user shadow lock. A missing organization lock is valid: consumers resolve the organization requirements without persisting a lock, while `lock --scope organization` remains available to administrators that want exact organization-owned resolutions.

There is deliberately no committed snapshot of the complete effective environment. Such a file would absorb organization, user, and local state into the repository, vary between machines, disclose unrelated source identities, and conflict with the existing machine/project materialization split.

Runtime composition is:

1. load and validate each config layer;
2. load the lock owned by each present layer;
3. resolve that layer's remote inputs through its lock;
4. compose the resulting locked catalogs in normal layer order;
5. apply existing selectors, exclusions, required/default rules, collection semantics, and collision handling;
6. materialize the selected resources.

## Configuration after lock extraction

Git registry config retains acquisition and selection intent:

```yaml
registries:
  - url: https://git.example.com/agents/resources.git
    ref: main
    path: skills
    source_harness: claude
    skills:
      include: [review]
```

Marketplace config retains endpoint identity and direct requirements:

```yaml
marketplaces:
  - url: https://marketplace.example
    instance_id: mp_123
    alias: public
    resources:
      - source_id: source_456
        kind: skill
        name: review
        installed_name: review
```

An optional `revision` on a Marketplace requirement is an exact constraint. Omission means that a compatible existing lock is retained by sync and may be advanced by update. Collection-generated leaf pins and `selected_by` edges are generated resolution state and therefore exist only in the lock.

Plain filesystem directory registries remain live and unlocked. Their paths and contents are not copied into lockfiles, and ordinary local edits do not make a lock stale. A hostless `file://` URL whose decoded path ends in `.git` is instead a supported remote Git registry and can be locked. This matches the development-path behavior of Go, npm, and uv without turning local directory authoring into a lock-update loop.

## Lock schema

The generated format is deterministic YAML with an independent schema version:

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
            support_files:
              guide.md: sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd

marketplaces:
  - url: https://marketplace.example
    instance_id: mp_123
    collections:
      - source_id: source_sets
        name: baseline
        revision: sha256:1111111111111111111111111111111111111111111111111111111111111111
        direct: true
        members:
          - source_id: source_456
            kind: skill
            name: review
            revision: sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
    resources:
      - source_id: source_456
        kind: skill
        name: review
        revision: sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
        acquisition: mirror
        archive_digest: sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff
        selected_by:
          - source_id: source_sets
            name: baseline
            revision: sha256:1111111111111111111111111111111111111111111111111111111111111111
      - source_id: source_private
        kind: mcp
        name: internal
        revision: sha256:2222222222222222222222222222222222222222222222222222222222222222
        acquisition: index-only
        source_commit: sha1:3333333333333333333333333333333333333333
        source_manifest_digest: sha256:4444444444444444444444444444444444444444444444444444444444444444
        direct: true
```

`scope` is required, must be one of `organization`, `user`, `repo`, or `local`, and must match the config path that selected the lock; copying a valid lock into another scope is a hard mismatch.

For Marketplace records, `revision` is the canonical content digest rather than a second alias for it. Every non-collection resource must have `direct: true`, one or more `selected_by` edges, or both. Every edge must resolve to an exact collection record in the same Marketplace entry. Collection records preserve exact direct member edges, including nested collections; cycles, missing records, and one coordinate resolving to conflicting revisions are invalid. Expanding each direct collection graph must produce exactly the non-collection records that name that root in `selected_by`, with no missing or extra leaf, and every non-direct collection record must be reachable from at least one direct root. Exact duplicate resources and edges are deduplicated deterministically. Mirror resources require `archive_digest` and forbid source-occurrence fields. Index-only resources require an algorithm-qualified `source_commit` and `source_manifest_digest` and forbid `archive_digest`. Collection records have no materialized archive.

Marketplace endpoint instance IDs, Source IDs, kinds, names, and collection names are safe single segments: they must be non-empty, contain no leading or trailing whitespace, whitespace, control characters, `/`, `\\`, or the exact path components `.` and `..`. Collection validation bounds one Marketplace graph to 100,000 member edges, 250,000 expansion steps, and depth 100. Lock YAML is bounded to depth 100 and 100,000 nodes and rejects aliases, merge keys, duplicate fields, and conflict markers.

### Identity rules

Git records are grouped by secret-free canonical URL and declared ref because those requirements share one exact commit. Configured roots remain separate by normalized slash path and source layout. The lock records the detected source layout when config uses autodetection.

Direct Git has no stable Source ID, so changing its canonical origin creates a different requirement. Lock identity uses a versioned canonicalizer shared by planner matching, serialization, acquisition, and immutable-cache keys; allowlist host/path normalization remains a separate policy operation and never defines source identity.

The v1 canonicalizer accepts validated `http`, `https`, and `ssh` URLs, hostless `file://` URLs, and `user@host:path` SCP-style SSH syntax. It trims surrounding whitespace; rejects controls, fragments, query strings, URL passwords, encoded separators, and dot-segment traversal; lowercases scheme and DNS host; removes default ports; cleans repeated path separators; and removes one trailing slash and one terminal `.git`. Hostless `file://` URLs are canonicalized as absolute file origins; configuration treats one whose decoded path ends in `.git` as a lockable Git registry, while a plain directory remains live and unlocked. HTTP(S) user information is omitted as authentication data. SSH usernames are retained because they can change repository identity. An SSH URL remains `ssh://[user@]host[:port]/absolute/path`; SCP syntax becomes the distinct identity form `git+scp://user@host/relative/path` and is never equated with an SSH URL. HTTP, HTTPS, SSH, SCP-style, and file origins remain distinct. The lock never stores passwords, tokens, or config-only credential helpers. Equivalence and non-equivalence fixtures are normative and must be shared across lock matching and cache tests rather than copied into subsystem-specific tables.

Marketplace identity reuses the existing tuple:

1. canonical endpoint and immutable Marketplace instance ID;
2. immutable Source ID;
3. resource kind;
4. source-local resource name;
5. content-addressed resource revision.

Aliases, source slugs, current index-only acquisition origins, assessments, and revocation declarations are not immutable identity.

Commit and content identifiers are algorithm-qualified. Lock schema v1 accepts `sha1:<40 lowercase hex>` and `sha256:<64 lowercase hex>` for direct Git. The Git acquisition adapter parses the typed value, passes only the raw hexadecimal OID to Git, queries the repository object format, requires it to match the declared algorithm, and verifies that checkout `HEAD` returns the same full OID. Unsupported algorithms and abbreviated OIDs fail before checkout. Marketplace index-only remains limited to `sha1` until its protocol explicitly supports another Git object format; the adapter converts between the lock's typed value and the current protocol's raw forty-character field at that seam.

### Deterministic bytes

The writer uses fixed struct field order and explicitly sorts:

- sources by canonical URL, then ref;
- roots by path and source layout;
- resources by kind and name;
- Marketplace endpoints by instance ID and canonical URL;
- Marketplace records by Source ID, kind, name, and revision;
- collection edges and support-file paths lexically.

Digests are canonical lowercase values. Artifact paths always use `/`. The writer avoids rewriting unchanged bytes and publishes changed files through validated temporary files and atomic rename. Lockfiles contain no credentials, cache paths, plain-directory paths, verification markers, or transient policy decisions. A lock may contain a canonical hostless `file://` origin when configuration identifies a Git registry by its `.git` suffix.

## Freshness and minimal reconciliation

Lock freshness is structural, not based on whether an upstream ref moved.

A lock requires reconciliation when:

- a writable lock is absent;
- a remote requirement was added or removed;
- a Git URL, ref, root path, or explicit source layout changed;
- a Marketplace coordinate or exact revision constraint changed;
- a collection requirement's locked closure is incomplete;
- a record conflicts with another record or uses an unsupported schema.

A branch or tag moving upstream does not make the lock stale. A changed selector or target harness also does not require network resolution when the corresponding Git root's complete inventory is already locked. Selection is applied after locked catalogs are composed.

Minimal reconciliation preserves every compatible resolution, resolves only added or invalidated requirements, and removes orphan records. It never upgrades a compatible resolution merely because a newer source commit or Marketplace revision exists.

## Command contract

### `agent-marketplace lock`

The lock command is the primitive non-materializing operation:

```text
agent-marketplace lock [--scope user|repo|local|all]
agent-marketplace lock --check [--scope user|repo|local|organization|all]
agent-marketplace lock --scope organization
agent-marketplace lock --upgrade [<marketplace-locator>...] [--scope user|repo|local|organization|all]
agent-marketplace lock --upgrade [<name>...] --kind <kind> --registry <url> [--ref <ref>] [--path <path>] [--scope user|repo|local|organization|all]
agent-marketplace lock --upgrade --registry <url> [--ref <ref>] [--path <path>] [--scope user|repo|local|organization|all]
```

`lock` minimally reconciles writable locks. `lock --check` exits nonzero if reconciliation would change a lock and writes nothing. `lock --upgrade` deliberately seeks newer compatible resolutions for all or selected requirements. Omitted scope and `--scope all` both mean the effective writable user, repo, and active local layers; `all` never includes organization. Organization scope is writable or upgradable only when named exactly and permissions and trust checks allow it, making the explicit form suitable for organization deployment pipelines. That administrative form may create a missing organization lock only when the config and parent directory already pass trust checks and the published lock's owner and mode are verified before success is reported.

Marketplace targets use the existing `alias:source/kind/name[@sha256:...]` or browser-URL locator. A revision-qualified requirement is exact and reports “constrained” rather than advancing. If one Marketplace requirement exists in several selected writable layers, every match is updated unless `--scope` narrows it.

Direct-Git targets use `--registry` with optional declared `--ref`, root `--path`, and scope qualifiers. Omitting names updates every matched source occurrence. Names require `--kind`, must exist in the embedded locked inventory, and update every matched root in the selected scopes; ambiguity between distinct source occurrences is a hard error with the qualifiers needed to disambiguate it. A targeted Git resource still advances its entire containing source occurrence because the commit is the indivisible acquisition unit, and output states that expansion explicitly. Marketplace positional locators and `--registry` targeting cannot be mixed in one invocation.

### `agent-marketplace sync`

Normal sync:

1. minimally reconciles writable locks;
2. validates the externally managed organization lock when present without changing it;
3. acquires and verifies exact locked content;
4. applies effective selection;
5. materializes.

A missing writable lock is bootstrapped automatically. A missing organization lock leaves that layer unlocked and is never created implicitly; a stale organization lock is a hard error with administrator remediation.

`sync --locked` requires every lock to be present and fresh and performs no config or lock writes. It may use the network to acquire exact locked content.

`sync --check` implies `--locked` and checks both lock freshness and materialized-output drift without changing config, locks, ownership state, or output. Disposable cache warming remains permitted online.

`sync --offline` implies `--locked`, performs no remote access, and requires every exact locked object to be present in a verified cache. It retains the existing warning that offline Marketplace policy knowledge may be stale.

There is no `--frozen` flag. uv uses that word for trusting a lock without freshness validation, which is too easy to misread as stricter than `--locked`.

### `agent-marketplace update`

`update` composes `lock --upgrade` with synchronization and accepts the same Marketplace positional locators or direct-Git `--registry`/`--kind`/`--ref`/`--path` targeting forms. Its optional `--scope` accepts `user`, `repo`, `local`, or `all`; organization changes use the non-materializing administrative lock command. Without targets it advances every eligible resolution in all effective writable scopes: user, repo, and active local. It never changes the managed organization lock.

`update --bump` first edits matching declared Git tag constraints, then resolves and commits affected lock entries. All candidate sources, manifests, collection graphs, and artifacts are acquired and verified before config or lock mutation.

Once valid config and locks commit, they remain committed even when later harness materialization fails. The command reports that dependency resolution succeeded but synchronization failed, and a later sync retries the exact committed resolution. This is a deliberate change from rolling back valid Marketplace pins after an unrelated materialization failure.

### Install and remove

Install and remove edit human-authored requirements, minimally reconcile the affected locks, and synchronize. They preserve compatible unrelated resolutions. The requirement and lock commit remains valid if subsequent materialization fails.

An unqualified Marketplace install omits config `revision` and records the selected revision only in the lock, so later update may advance it. An explicitly revision-qualified locator persists that digest as the config requirement's exact `revision`; ordinary and targeted updates report it as constrained and never advance it unless the user first changes the requirement.

### Read-only catalog commands

`status`, configured `list`, and TUI startup never reconcile or rewrite locks. They validate layer/lock agreement and report missing, stale, untrusted, or malformed locks as diagnostics. `status` renders lock metadata even when the exact cache entry is absent and marks materialization drift unknown when it cannot inspect content. Configured `list` and the TUI browse the complete inventories embedded in valid locks plus live local registries, so browsing does not advance mutable refs. Hard lock failures are contained to the affected layer for display, but configured `list` and `status` return nonzero when their effective view is incomplete; the TUI remains open and marks the affected source blocked. TUI refresh remains an explicit confirmed update action.

Ad-hoc `list --from` is intentionally unlocked discovery: it may fetch the requested ref, labels the result as live, and does not affect configured locks. Marketplace `search` likewise remains live discovery rather than installed-state resolution. Commands that need materializable bytes use exact cache entries or report their absence; read-only commands do not populate authoritative files or ownership state.

## Modules and data flow

The lock subsystem is a deep module whose primary pure interface is conceptually:

```go
Plan(layers, locks, operation) -> ResolutionPlan
```

The plan classifies requirements and records as retained, added, invalidated, upgraded, or removed. It contains unresolved acquisition requests and deterministic desired documents but performs no filesystem, network, process, or materialization mutation.

The implementation separates four responsibilities:

1. **Schema module:** strict decoding, validation, normalization, deterministic encoding, and compatibility errors.
2. **Planner module:** pure requirement/lock matching and minimal or upgrade planning.
3. **Acquisition adapters:** exact Git, Marketplace mirror, and Marketplace index-only resolution and verification.
4. **Commit module:** cross-process serialization, compare-and-swap publication, journal recovery, and unchanged-byte avoidance.

Pipeline orchestration composes these modules and passes locked catalogs through the existing resolver and materializer. Callers do not manipulate lock records or cache paths directly.

## Immutable Git acquisition and cache

The current mutable `URL/ref` checkout cannot back two locks that retain different commits. Git caching therefore separates a shared source object store from immutable commit-addressed snapshots.

A symbolic ref may be fetched into the source object store only during missing/invalid reconciliation or explicit update. Exact synchronization fetches the locked object when absent but never resolves the current ref tip as a fallback. A materialization snapshot is keyed by canonical source identity plus algorithm-qualified commit. When that exact snapshot is consumed, the catalog key is likewise commit-qualified (`canonical-source@algorithm:commit:path`), while mutable catalogs remain keyed by URL, ref, and root path.

Before a snapshot becomes trusted, acquisition verifies:

1. checkout `HEAD` equals the locked commit;
2. the checkout and roots contain no path or symlink escape;
3. every root resolves to the locked source layout;
4. ingestion reproduces the complete source revision manifest;
5. the manifest digest and every resource/archive/support-file digest match the lock.

The cache remains disposable. Deleting it has no effect on lock authority. Online synchronization can reconstruct it from exact identities; offline synchronization fails closed when the exact verified snapshot is absent.

Cache writes and garbage collection require cross-process serialization. Snapshot publication uses temporary directories and rename so readers never observe a partially populated immutable entry. Read-only exact-snapshot verification and consumption hold a shared exact-cache namespace lease for the entire consume operation, so GC cannot remove a verified snapshot between validation and use. Exact Git cache lock handles are disposable files under the cache lock directory and are never lock authority.

## Marketplace acquisition and policy

Mirror locks include both semantic content revision and archive digest. Index-only locks include Marketplace instance, Source ID, exact source commit, source-manifest digest, and resource revision. Acquisition modes never fall back to each other.

Online use of a locked Marketplace resource still validates current endpoint identity, assessment, and effective revocation. A lock proves which content was selected; it does not certify that content remains authorized. Offline mode uses verified cached policy metadata and reports that newer revocations cannot be observed.

Mutable acquisition origin is obtained from current Marketplace metadata, checked against `allowed_registries`, and used only to reacquire the exact locked index-only occurrence. It is not persisted as immutable lock identity.

## Commit and recovery model

Network and content verification complete before authoritative file mutation. A mutating operation then:

1. acquires advisory locks for affected config/lock pairs in sorted canonical-path order;
2. rereads inputs and rejects concurrent changes through generation checks;
3. stages every desired file with validated permissions and containment;
4. persists a transaction journal in durable state;
5. publishes each file atomically;
6. marks the transaction complete and removes recovery data;
7. releases advisory locks;
8. begins materialization.

Every CLI path that reads or writes these files participates in the same advisory-lock and recovery protocol. The journal permits the next invocation to complete or restore a process that exited between per-file renames. Journal publication records recovery authority before the journal-directory fsync; if that fsync fails, the operation reports the error but retains the renamed journal and staged files for the next invocation rather than deleting its recovery evidence. Recovery never guesses from timestamps and never treats malformed state as authority. Per-destination handles are named `.<basename>.dependencytxn.lock` beside the destination; repository `.agents/*.dependencytxn.lock` handles are transient and remain covered by the repository ignore rule rather than being treated as lockfiles.

Transaction destinations reject symlinked ancestors and non-regular paths after lexical normalization. The supported final config-file symlink is accepted only when its regular target remains adjacent to the logical config path; ancestor symlinks cannot redirect a config/lock transaction into another layer.

The dependency commit is the transaction boundary. Materialization is intentionally outside it: multiple harness roots cannot be made one filesystem transaction, and a valid lock should not be discarded because one target compiler or destination fails.

## Migration

Existing writable Marketplace pins migrate automatically during the first normal reconciliation:

1. validate the old config and identify direct requirements and collection roots;
2. use current exact pin data to seed candidate locked resolutions;
3. fetch any missing metadata required to record archive and occurrence integrity;
4. verify the complete candidate lock;
5. remove generated leaves and exact resolution fields from config;
6. commit simplified config and lock through one journaled transaction;
7. synchronize exact locked content.

Legacy exact revisions seed the initial lock but do not become permanent exact constraints. The existing format cannot distinguish an explicitly requested revision from the exact revision generated by a latest-version install, and the current update command advances both. This migration preserves that behavior. The syntax-aware config editor preserves comments, key order, scalar style, unrelated fields, and final newline while removing only generated resolution fields and collection leaves.

A failure before the commit leaves original config bytes unchanged. Managed organization config never migrates implicitly. Administrators use explicit organization-scope lock generation and deploy the resulting config/lock pair.

The implementation may accept the legacy config shape only as migration input. It must not preserve dual authority indefinitely.

## Diagnostics

Stable coded diagnostics distinguish at least:

- `lock.missing`, `lock.stale`, `lock.trust`, and `lock.change-required` for missing, mismatched, untrusted, or would-change locks;
- `lock.snapshot-unavailable`, `lock.manifest-mismatch`, and `lock.lookup` for missing exact cache content, integrity mismatch, or ambiguous exact catalog selection;
- `organization.trust.*` failures and `org-config-*` managed-installation warnings for symlinks, unsafe ancestors, ownership/permissions, malformed or scope-invalid organization locks; filesystem findings remain reportable even when the config cannot be parsed;
- lock/config mismatch under `--locked`;
- constrained exact requirement skipped by update;
- unsupported lock schema or Git object algorithm;
- malformed or textually conflicted generated lock;
- duplicate immutable identity with conflicting values;
- unavailable exact Git commit or Marketplace artifact;
- Git source-manifest mismatch;
- resource, archive, or support-file digest mismatch;
- forbidden credential or nonportable path in a lock;
- interrupted transaction recovered or recovery blocked;
- materialization failure after a successful dependency commit.

Malformed, unsupported, ambiguous, or integrity-invalid locks fail before materialization. No error path silently follows a mutable ref, switches Marketplace acquisition mode, or treats an unverified cache entry as locked content.

## Verification

### Schema and planner

- Independently authored byte-exact golden tests for each scope, mirror/index-only mode, nested collection graph, and mixed direct/collection selection.
- Round-trip, field-invariant, duplicate-conflict, and unsupported-version tests.
- Table-driven retained, added, removed, invalidated, exact-constraint, and targeted-upgrade cases across duplicate layers and roots.
- Shared canonical-URL equivalence/non-equivalence and typed Git OID fixtures.
- Property tests that config input ordering cannot change output bytes.
- Positive controls for every no-op, filtering, redaction, and rejection case.

### Git lifecycle

Local bare repositories prove:

- first sync creates a lock at current branch tip;
- moving the branch does not change a subsequent sync;
- empty-cache sync reacquires the exact locked commit;
- SHA-1 and SHA-256 object formats accept matching typed OIDs, reject mismatches, and verify full `HEAD`;
- update advances the source occurrence and manifest;
- targeted resource update expands to the containing Git source;
- selector-only changes reuse the locked inventory;
- `--locked` rejects requirement drift;
- offline mode requires the exact verified snapshot;
- digest mismatch blocks materialization without changing output.

### Marketplace lifecycle and migration

Tests cover mirror and index-only acquisition, direct requirements, revision-qualified versus unqualified install, exact-constraint update refusal, collections with nested/shared leaves, installed names, automatic writable migration, explicit organization migration, current revocation checks, and offline policy staleness. Failed migration preserves exact prior config bytes and creates no lock or temporary residue.

### Transaction and security

Fault injection interrupts every stage and proves deterministic recovery. Multi-process tests update overlapping layers and verify serialization without deadlock. Organization tests cover missing, symlinked, non-regular, wrongly owned, group/other-writable, and unsafe-ancestor locks and prove that none retain managed authority. Security tests cover credentials, URL canonicalization, traversal, symlink escapes, malformed YAML, merge markers, conflicting identities, unsupported algorithms, and cache tampering.

### E2E and performance

Declarative E2E cases cover standard sync, update, targeted updates, install, remove, locked, check, offline, migration, configured locked browsing, and intentionally live `list --from`. Standalone process tests cover concurrency, crash recovery, monorepo-local locks, and managed organization files.

Benchmarks are required for unchanged-lock planning, source-manifest generation, and normal sync because they are user-visible hot paths. Baseline and after measurements follow the repository performance policy.

## Phased implementation

1. Add the versioned schema, pure planner, deterministic writer, lock command, diagnostics, and planning benchmarks.
2. Add cross-process config/lock commits, durable journal recovery, and adversarial concurrency tests.
3. Add immutable direct-Git acquisition, commit-addressed snapshots, manifest verification, GC reachability, and lifecycle benchmarks.
4. Extract Marketplace requirements from exact pins, add collection lock closure, and implement automatic writable migration.
5. Integrate sync, update, install, remove, status, check, offline, and TUI behavior; finish E2E coverage, documentation, and legacy compatibility removal.

A phase may merge only when its exposed command path is internally coherent. The product must not claim reproducible synchronization while any remote direct-registry path still advances a symbolic ref during ordinary sync.
