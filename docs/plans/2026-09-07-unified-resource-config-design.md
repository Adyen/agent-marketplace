# Unified resource configuration design

**Status:** Partially implemented. The repository ships the Git/directory v4 parser, include composition, runtime adapter, and explicit migration for representable standalone legacy files, plus the strict `agent-marketplace-lock/v2` exact-pin slice: verified-cache evidence reconstruction, journaled config/lock publication, and the read-only lock-backed v4 Marketplace adapter are implemented. Hosted Marketplace acquisition, update, dynamic selectors, and v4-aware editing remain unimplemented.

**Prerequisite:** Broader hosted Marketplace consumer behavior beyond the implemented exact-pin slice still requires the remaining lock/acquisition lifecycle described below.

**Audience:** Client implementers and reviewers. This document records the v4 syntax and the implemented relationship with acquisition locks, while marking the hosted Marketplace portions that remain proposals.

## 1. Scope and agreed decisions

Config records sources and installation intent. A layer-owned lock records exact acquisition results. Materialized harness files and aggregate ownership state remain separate from both.

| Concern | Decision |
|---|---|
| Source declarations | One named `sources` map for Git, local directories, and Marketplace endpoints |
| Ordinary selections | Short resource locators grouped by source under `resources` |
| Advanced selections | A group can expand to an `include`/`exclude`/`tags`/`all` mapping |
| Per-resource options | A string item can expand to a locator-keyed mapping |
| Source binding | A request binds within its own config layer, including that layer's explicit includes |
| Marketplace identities | Automatically discovered instance and registry IDs live only in the owning lock |
| Include composition | Named requests accumulate; higher-precedence decisions win for the same locator |
| Marketplace dynamic selectors | `all` and `tags` require an explicit list of registry slugs |
| Migration | Consent-based proactive migration to `agent-marketplace/v4`; existing v2/v3 configs remain supported |

This design does not change harness configuration precedence, source allowlist authority, materialization modes, aggregate ownership, trust handling, resource prerequisites, or existing collision policy. It does not introduce dependency installation or semantic-version ranges.

Moving pin fields out of config is valid only for the implemented exact-pin migration backed by `agent-marketplace-lock/v2`. Broader Marketplace selections in this schema require the remaining hosted lock extension.

## 1.1 Implementation status

The current repository implements the Git/directory and exact-pin migration subsets of this design:

### Implemented

- Strict `agent-marketplace/v4` parsing and normalization, including source-bound resource groups, compact and expanded selectors, per-resource options, bounded exclusions, duplicate/unknown-field rejection, version boundaries, and deterministic include composition.
- Git and live-directory source handling through the existing loader, cache, resolver, lock, and materialization pipeline. Git sources reuse the existing exact-snapshot lock foundation; directories remain live and are never added to locks.
- Source safety checks, including shared Git URL/ref validation, relative-directory containment and symlink escape checks, source-kind preservation, and fail-closed handling for Marketplace sources that lack the hosted lock integration.
- Ordered selection semantics for explicit requests, tags, `all`, exclusions, required protection, harness narrowing, collection propagation, and source-local provenance in the legacy runtime adapter.
- The `agent-marketplace-lock/v2` codec and its exact-pin migration slice: v2 retains v1 Git records and adds deterministic Marketplace endpoint/registry/resource/selection records; legacy Marketplace pins migrate offline from verified cache evidence into the owning layer lock, published with the config in one journaled dependency transaction; a read-only adapter hydrates explicit v4 Marketplace requests from the matching normalized v2 lock and fails closed on missing/mismatched state.
- Consent-based proactive v4 migration: `migrate` defaults to v4, the TUI reuses its migration modal, and interactive CLI startup asks before converting detected writable v2/v3 roots. Migration is semantic/selection-preserving, regenerates canonical YAML, preserves file modes, leaves locks untouched for Git/directory-only inputs, and refuses include trees, unproven Marketplace pins, or other inputs that cannot be represented without semantic loss without writing changes. Non-interactive startup warns and continues with legacy reads; `init` creates v4 roots directly.
- Regression, resolver, migration, CLI, E2E, and hot-path benchmark coverage for the implemented parser/adapter and migration behavior.

### Remaining

- The hosted Marketplace lock lifecycle beyond the implemented exact-pin slice: dynamic-selector snapshots, hosted acquisition, update, explicit identity rebind, and garbage-collection integration. The v2 lock codec, endpoint instance bindings, registry IDs, exact resource revisions, collection selection edges, mirror/index-only migration records, integrity data, and old-reader refusal are implemented for migration-produced state.
- Marketplace acquisition adapters and their integration with config-plus-lock transactions, immutable-cache leases, garbage collection, revocation, capability consent, update, check, and recovery behavior.
- V4-aware `install`, `remove`, CLI/TUI resource-request editing, comment-preserving node-level writes, and common editor/normalization plumbing. Existing editing paths remain legacy-oriented; authored v4 Git/directory configs can be read and synchronized, but the editor is not yet a complete v4 authoring surface.
- Marketplace endpoint registration, Marketplace resource selection, dynamic selectors, and Marketplace `list`/`status`/`sync`/`update` behavior beyond the read-only lock-backed adapter. Explicit v4 Marketplace requests resolve only when backed by a matching migrated v2 lock; unsupported group/request policy still fails closed.
- Semantic/selection-preserving migration support for included legacy trees. These inputs remain refused or require separately reviewed remediation; consented startup migration warns and leaves the legacy reader active.
- The complete hosted acceptance matrix and release gate covering identity changes, missing versus corrupt locks, complete enumeration, partial-query failure, transactional publication, concurrent edits, and cross-scope behavior beyond the exact-pin slice.

No hosted Marketplace feature is activated by the schema alone.

## 2. Canonical config shape

### 2.1 Ordinary configuration

```yaml
apiVersion: agent-marketplace/v4

sources:
  shared:
    git: https://git.example.com/agents/resources.git
    ref: main
  company:
    marketplace: https://marketplace.example.com

resources:
  shared:
    - skill/review
    - command/deploy
  company:
    - platform-tools/skill/debugging
    - platform-tools/collection/onboarding
```

Source names are author-chosen references, not durable acquisition identities. They do not imply installation scope. Declaring a source without selecting resources makes it available for discovery but installs nothing.

### 2.2 Advanced configuration

```yaml
apiVersion: agent-marketplace/v4

sources:
  shared:
    git: https://git.example.com/agents/resources.git
    ref: main
    path: skills
    source_harness: claude
  company:
    marketplace: https://marketplace.example.com
  workspace:
    directory: ./workspace-resources

resources:
  shared:
    include:
      - skill/review
      - command/deploy:
          as: deploy-platform
    tags: [platform]
    exclude:
      - skill/legacy-review
  company:
    registries: [platform-tools, engineering]
    tags: [onboarding]
    include:
      - security/skill/review
      - platform-tools/collection/onboarding:
          revision: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    exclude:
      - platform-tools/command/deploy
  workspace:
    - skill/local-testing
```

The relative directory is resolved from the file declaring the source, not from the shell's working directory. In a root `.agents/config.yaml`, `./workspace-resources` therefore names `.agents/workspace-resources`.

### 2.3 Root and fragment schema

| Field | Type | Required | Default | Meaning |
|---|---|---|---|---|
| `apiVersion` | String | Yes for v4 roots | None | Exactly `agent-marketplace/v4` |
| `include` | String or string list | No | No includes | Existing explicit local-file include mechanism |
| `sources` | Map of source names to declarations | No | Empty map | Sources available within the owning layer |
| `resources` | Map of source names to resource groups | No | Empty map | Installation requests and selection rules |

Existing non-selection fields, such as `harnesses`, `materialization`, `backup`, `logging`, `telemetry`, `allowed_registries`, and `allowed_marketplaces`, retain their existing types and layer restrictions. This is not permission to declare them in additional layers.

V4 roots include `config.local.yaml`; they carry the version explicitly. Unversioned fragments inside a v4 include tree inherit v4. A fragment with an explicit version must declare v4. Cross-version composition is supported between config layers, not inside one include tree.

V4 rejects legacy top-level `registries`, `marketplaces`, `skills`, `commands`, `subagents`, `hooks`, `mcp`, `collections`, and `tags`. It also rejects nested legacy selectors inside source declarations. A document cannot mix both installation schemas.

Unknown keys, duplicate mapping keys, wrong node types, multiple YAML documents, and malformed resource items are errors. YAML merge keys or aliases must not provide an alternative include or override mechanism.

## 3. Source declarations

### 3.1 Names and bindings

A source name matches `[a-zA-Z0-9][a-zA-Z0-9._-]*`, is case-sensitive, and contains no slash or colon. The same name may be used independently in different config layers. Every resource group must have a corresponding source declaration within its own layer's include tree.

Each declaration has exactly one acquisition discriminator: `git`, `directory`, or `marketplace`. An HTTPS URL is not sufficient evidence to infer an acquisition method.

### 3.2 Source fields

| Field | Type | Applies to | Required | Default / meaning |
|---|---|---|---|---|
| `git` | String | Git | Yes | Git origin, validated by #123's canonicalizer |
| `ref` | String | Git | Yes | Human-requested branch, tag, or explicit commit; no implicit `main` assumption |
| `path` | String | Git | No | Empty means repository root; otherwise a validated slash-separated subdirectory |
| `source_harness` | String | Git, directory | No | Autodetect source layout when omitted |
| `directory` | String | Directory | Yes | Live local directory; no acquisition lock |
| `marketplace` | String | Marketplace | Yes | Canonical Marketplace endpoint URL |

Fields belonging to another source type are errors. In particular, Marketplace declarations do not accept `ref`, `path`, or `source_harness`; directory declarations do not accept `ref`.

Existing local-directory resolution, containment, and symlink-safety requirements remain in force. Local paths never enter acquisition lockfiles. A hostless `file://` Git origin must satisfy #123's lockable-origin rules; a live filesystem source uses `directory` explicitly.

Two paths or refs from the same repository can have different source names. Git acquisition still coalesces compatible requests by canonical URL/ref, retaining separate root manifests.

No generated `instance_id`, `registry_id`, commit result, digest, acquisition mode, or cache path is written into source declarations. A Git commit explicitly authored as `ref` is a constraint, not a generated result, and remains in config.

### 3.3 Duplicate declarations within an include tree

Equivalent declarations of one source name coalesce. Conflicting declarations of that name are errors rather than silently retargeting requests from an included file. Equivalence uses the appropriate canonical origin, requested ref, normalized root, and declared source layout; relative directories are resolved relative to their declaring files first.

A genuinely different source uses another name. Config editors do not repoint an included declaration by introducing a conflicting root declaration. The diagnostics identify both declaring files.

Multiple names may refer to equivalent source declarations. They share acquisition records where compatible, but retain their authored selection groups and provenance. A source-name-only rename does not change acquisition identity or advance content.

## 4. Resource groups

### 4.1 Compact and expanded forms

```yaml
resources:
  shared:
    - skill/review
    - command/deploy
```

This is exactly equivalent to:

```yaml
resources:
  shared:
    include:
      - skill/review
      - command/deploy
```

A compact empty list contributes no named requests. It does not clear requests or selector fields inherited from earlier files in the same layer.

The expanded group accepts the following fields. `required`, `harnesses`, and the exclusion-pattern grammar make existing policy expressible without placing selections back into source definitions.

| Field | Type | Default | Meaning |
|---|---|---|---|
| `include` | Resource-item list | No contributions | Explicit named requests |
| `exclude` | Exclusion-string list | No contributions | Source-qualified exclusions across selection reasons |
| `tags` | String list | No tag selection | Match any listed tag in resource metadata; exact, case-sensitive matching |
| `all` | Boolean | `false` | Select every supported resource kind in the applicable source scope |
| `registries` | String list | Absent | Marketplace registry slugs bounding dynamic selection; invalid for Git/directory sources |
| `required` | Boolean | `false` | Preserve mandatory-selection protection for resources selected by this group |
| `harnesses` | String list | Inherit applicable target set | Narrow the applicable target set; never widen it |

Omitted fields express no override during include composition. An explicit empty `tags` list disables inherited tag selection. An explicit `all: false` disables inherited all-selection. Neither removes inherited named requests. An explicit empty `harnesses` list selects no targets.

`required` is selection protection, not a selection expression: `required: true` alone selects nothing in v4. Legacy implicit all-selection is translated to explicit `all: true` during a semantic/selection-preserving migration. Required selections remain immune to exclusions and consumer removal under the existing authority rules.

There is no v4 `default` field. Its legacy selection effect is represented by the appropriate explicit selectors, without adding mandatory protection.

### 4.2 Locator grammar

| Source | Locator |
|---|---|
| Git or directory | `<kind>/<name>` |
| Marketplace | `<registry-slug>/<kind>/<name>` |

Kinds are exactly `skill`, `command`, `subagent`, `hook`, `mcp`, and `collection`. The config kind is `mcp`; the Marketplace adapter translates it to any protocol spelling such as `mcpserver`. Kind is mandatory; there is no implicit skill shorthand.

Registry slugs use the existing Marketplace slug grammar. Names are nonempty resource-name segments, not filesystem paths. Slashes, control characters, traversal segments, and wildcard tokens are not legal inside an explicit name. Names and slugs are case-sensitive according to their source contracts. Unsupported names produce a diagnostic rather than being rewritten or URL-decoded into another identity.

Config locators omit the source prefix because their group supplies it. The corresponding CLI locator can prefix the source name, for example `shared:skill/review` or `company:platform-tools/skill/debugging`. The editor still writes the grouped form, not redundant prefixes.

### 4.3 Per-resource options

Each `include` item is either a locator string or a mapping with exactly one locator key and an options mapping:

```yaml
resources:
  shared:
    - command/deploy:
        as: deploy-platform
  company:
    - platform-tools/skill/review:
        revision: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
```

| Option | Type | Default | Meaning |
|---|---|---|---|
| `as` | String | Resource's name | Installed name, subject to existing installed-name/path validation |
| `revision` | String | No exact user constraint | Exact canonical `sha256:<64 lowercase hex>` Marketplace resource or collection revision |

`revision` is invalid for Git/directory resources. Git resources share the source's requested `ref`; independently requested refs use distinct source declarations. Semantic-version constraints and magic revision strings such as `latest` are not part of v4.

`as` is invalid for collections because collections do not materialize. It does not rename the source coordinate or change content identity. Ordinary installation does not manufacture a `revision` option from a resolved result. An explicit revision request is retained even though it happens to be a hash.

For Git and directory sources, the current legacy runtime only accepts an omitted or same-name `as` value; a differing installed alias is parsed but rejected with `v4.runtime-option`. Honoring differing aliases remains part of the future hosted acquisition/editor boundary.

An empty options map is semantically identical to a string item; editors emit the string when no options remain. Null/scalar option values and multi-key resource items are invalid. Duplicate locators within one include list are errors, even if their options match. One source group cannot install two different revisions of the same locator by repeating it.

### 4.4 Dynamic selection and exclusions

Selection candidates are the union of named requests, tag matches, and all-selection, followed by collection expansion and applicable exclusions. An explicit request supplies its options when a resource is also dynamically selected. Required-selection and scope protections remain applicable throughout expansion and exclusion.

A Marketplace group with `all: true` or a nonempty `tags` list must have an effective, nonempty `registries` list. Registry entries are explicit slugs, not wildcards. A source-wide query is not an implicit fallback. `registries: []` is invalid while dynamic selection is active.

`registries` bounds only dynamic selection. A fully qualified explicit request may select another registry from the same endpoint. A `registries` list alone selects nothing. Git/directory dynamic selectors are scoped to their configured root.

Exclusions are strings, not resource-option mappings. In addition to exact locators, the proposed grammar supports only these bounded wildcard forms:

| Source | Additional exclusion forms |
|---|---|
| Git/directory | `<kind>/*`, `*` |
| Marketplace | `<registry-slug>/<kind>/*`, `<registry-slug>/*`, `*` |

These forms express kind-level and source-level opt-outs without enumerating current resources. They are not general glob or regex syntax, and are not accepted in `include` or `registries`.

Exclusions match source coordinates, not installed aliases. They can suppress direct selections, dynamic matches, or collection-provided members from that source, subject to required protection and layer boundaries. An exclusion-only group can suppress an eligible lower-layer repo occurrence with the same canonical source identity; matching source names alone never establish that identity.

Excluding a collection excludes that selection reason and its derived membership, not a member's independent direct selection. Excluding a leaf suppresses that eligible leaf across selection reasons. Cross-source collection members are excluded through the group for their actual source.

## 5. Includes, scopes, and ownership

### 5.1 Include composition within a layer

Existing include traversal order remains: recursively included fragments first, then their containing file; later sibling includes have higher precedence than earlier ones. Existing cycle, path, and unreadable-fragment failures remain errors.

For resource groups bound to one source name:

1. Distinct named locators accumulate.
2. A higher-precedence explicit decision for a locator overrides a lower-precedence decision. An exact include can therefore override an earlier matching exclusion, including a wildcard exclusion.
3. Within one file, an applicable exclusion wins over an inclusion. Required selections remain protected.
4. When an explicit item overrides another item, its options replace the previous options as a whole. An omitted `revision` does not retain an earlier pin constraint.
5. `tags`, `all`, `registries`, `required`, and `harnesses` use the last explicitly declared value within the owning layer. Lists in these fields replace, rather than concatenate with, the earlier value.
6. Named selection provenance and file precedence survive normalization. Dynamic/collection-derived candidates do not invent an explicit override of an authored exclusion.

The selection engine must retain the ordered decisions needed to apply these rules; a plain union of include and exclude sets loses necessary information.

Policy from another consumer layer cannot clear a mandatory selection, even when that layer has higher config precedence. Within an owning layer, changing the policy fields is an edit to that layer's own declared policy, not cross-layer authority.

### 5.2 Self-contained source bindings

| Config owner | Source lookup | Acquisition lock |
|---|---|---|
| Organization | Organization root and its includes | `/etc/agent-marketplace/lock.yaml` |
| User | User root and its includes | `~/.agents/lock.yaml` |
| Repository | Repository root and its includes | `<repo>/.agents/lock.yaml` |
| Personal-local | Local root and its includes | `<repo>/.agents/lock.local.yaml` |

Source references bind before cross-layer resolution. Identical names in different layers neither override one another nor create a shared lock authority. User and repository occurrences may coexist at their respective materialization roots.

A repo install discovered through a personal source copies the necessary declaration into the repo layer before adding the request. It copies neither personal selections nor a personal lock as the repo's authoritative lock. Matching verified acquisition records can be reused only after validating them against the destination layer's requirements and policy.

If the destination name is already bound to another source, interactive installation requests an alternative name; noninteractive installation fails with an actionable diagnostic. It never overwrites the existing binding. Directory declarations copied between files must preserve their actual location rather than blindly copying a relative path; an unsafe or nonportable repo destination is refused for explicit correction.

Explicit includes can still refer to external files under existing rules. Such an include is an authored environment dependency; implicit lookup in unrelated user config is not.

Editors modify the selected root, not included fragments. Repo/local exclusions cannot suppress machine resources. Required resources cannot be suppressed through alias changes, scope changes, or collection indirection. Existing source/path/ref collision and overlap policies remain in force after normalization.

## 6. Acquisition lock contract

### 6.1 Layer and version boundaries

PR #123 remains the Git foundation: exact snapshots, source manifests, safe canonical URLs, layer-owned locks, config-plus-lock transactions, read-only checks, immutable-cache leases, and garbage-collection protection.

The repository lock is committed. The personal-local lock is ignored. `~/.agents/lock.yaml` contains user-layer acquisition state, not a database of every checkout's resources. Locks are not merged by config precedence, and a higher layer does not rewrite another layer's lock.

An intentionally absent organization Git lock retains #123's existing unlocked-authority behavior. That exception does not authorize unbound Marketplace installation. Organization Marketplace locks/bindings must be externally provisioned; consumer commands do not establish them by modifying organization state.

Config version and lock version are independent. Git-only v4 requirements can reuse valid `agent-marketplace-lock/v1` records without a format-only rewrite. V1 remains Git-only; strict `agent-marketplace-lock/v2` is now implemented for exact migration-produced Marketplace state, and old v1 readers reject it rather than dropping Marketplace state.

Because strict v2 intentionally lacks dynamic-selector and acquisition-update fields, any broader hosted wire extension must receive its own reviewed version (for example v3) with an exact layout, bounds, golden fixtures, and old-reader fail-closed behavior, rather than adding silently ignored fields to v2.

### 6.2 Required generated record content

| Record | Required content |
|---|---|
| Git acquisition | Canonical URL, requested ref, algorithm-qualified exact commit, configured roots, detected/declared source layout, source revision manifest and resource digests from #123 |
| Marketplace binding | Canonical endpoint, immutable instance ID, authored registry-slug bindings to immutable registry IDs |
| Marketplace resource | Instance/registry identity, kind/name, exact resource revision and independently appropriate artifact-integrity data |
| Mirror acquisition | Exact resource identity plus canonical artifact/archive digest and the verified immutable artifact reference |
| Index-only acquisition | Exact resource identity, registry commit, registry-manifest digest, and sufficient exact occurrence information for verified reacquisition |
| Collection resolution | Exact collection revision/manifest, direct collection includes, deduplicated exact leaf membership, and selection edges |
| Dynamic selector resolution | Normalized acquisition-affecting selector intent, bound registry scope, complete resolved membership, and exact referenced resource/collection revisions |

Resource-content identity, archive integrity, and registry-manifest identity are distinct facts. None may be substituted for another merely because all use SHA-256. Generated `direct`/`selected_by` bookkeeping is not written back to config; direct requests remain inferable from config and the locked selection graph.

A resource selected directly and through a collection has both selection reasons. Removing one collection drops only its reason; removing a direct request does not remove a leaf still selected by another retained reason. Explicit leaf exclusions remain intentional filters, not deletion of upstream collection manifests.

Lockfiles contain no credentials, absolute cache paths, local-directory snapshots, aggregate-ownership proof, approval markers, or durable claims that a revision remains unrevoked. Existing live revocation and capability-consent rules remain separate from content locking.

### 6.3 Identity binding and missing locks

Readable registry slugs stay in config. Existing Marketplace guarantees make old slugs permanent aliases; the lock retains their association with immutable registry IDs.

An existing binding is execution input. A changed Marketplace instance ID or a slug resolving to another registry ID is an error in both sync and ordinary update. Endpoint/identity migration requires an explicit rebind operation with new confirmation; it is not an automatic update fallback. The command syntax for rebind is outside this schema change.

Without a lock, config alone cannot prove a previously accepted immutable identity. `--locked` fails. A mutating Marketplace operation must establish and confirm a new binding explicitly; noninteractive operation requires the existing explicit consent mechanism, not a silent acceptance default. A missing/corrupt lock must never be described as preserving old trust. Corrupt present locks fail closed rather than being treated as absent.

Only identities needed for selected requests or dynamic scopes require installation bindings. An endpoint configured solely for discovery need not gain an acquisition record. Revocations, identity checks, and endpoint/source allowlists remain enforced at their appropriate acquisition boundaries.

### 6.4 Dynamic membership and deterministic updates

Git dynamic selectors evaluate the inventory at the owning exact Git revision. Marketplace dynamic selectors use their locked membership snapshot. Unchanged config plus compatible locks does not discover or select newly published content during ordinary sync.

`update` reevaluates eligible dynamic selectors and can change their membership. A changed dynamic selector also requires reconciliation. For Marketplace, a selector newly evaluated or semantically changed selects current eligible revisions inside its requested registry scope; untouched compatible selector snapshots and explicit requests retain their existing revisions. Conflicting exact requirements go through existing conflict policy rather than silently upgrading another request.

`as`, source display-name changes, YAML formatting, and target-harness narrowing are not acquisition upgrades. Normalized request comparisons exclude presentation-only details. Requested source/ref changes, exact revision constraints, and dynamic selection changes are acquisition-affecting.

Enumeration must be complete and bounded. Partial pagination, query failure, truncation, or inconsistent inventory traversal must not publish a shortened membership as authoritative and prune the omitted resources. A verified complete empty result is distinct from an incomplete result and may intentionally remove membership during a confirmed update. Existing revocation policy remains applicable. The acquisition extension needs a tested completeness/consistency contract for Marketplace enumeration; a search-results page alone is not sufficient evidence for an all-selector lock.

Reconciliation verifies every new artifact and collection member before publishing the new config/lock generation. New executable capabilities or policy-relevant changes retain the existing preview/consent boundary. Locks are deterministic and reused when semantically unchanged.

### 6.4 Exact legacy pin migration slice

The first hosted-lock implementation is deliberately limited to migrating exact legacy Marketplace state that is already backed by verified local cache evidence. It does not acquire missing content, evaluate dynamic selectors, update a revision, rebind an identity, or make the v4 editors Marketplace-aware.

A migrated Marketplace entry becomes a named v4 `marketplace` source. A valid unique legacy alias is retained as the source name; otherwise the migrator allocates a deterministic non-conflicting `marketplace`, `marketplace-2`, and so on. A legacy pin becomes an explicit v4 request when `direct: true` or when it has no `selected_by` edge. Collection-only leaves stay generated lock membership rather than becoming new direct requests. `installed_name` becomes `as`. A revision distinct from the content digest is retained as an explicit `revision` only when it satisfies the v4 exact-revision grammar; an unrepresentable requested revision is refused. The registry slug is the locator binding name, with the immutable registry ID used as the deterministic fallback for older pins that lack a slug.

The owning layer lock uses `agent-marketplace-lock/v2` when Marketplace state is present. V2 retains the v1 Git `registries` records and adds deterministic Marketplace records. The exact-pin slice uses this wire shape:

```yaml
apiVersion: agent-marketplace-lock/v2
scope: user
registries: []
marketplaces:
  - url: https://market.example
    instance_id: market-id
    registry_bindings:
      - name: tools
        registry_id: immutable-tools-id
    resources:
      - registry_name: tools
        registry_id: immutable-tools-id
        kind: skill
        name: review
        revision: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        content_digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        archive_digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
        acquisition: mirror
        selected_by:
          - registry_id: immutable-collections-id
            name: onboarding
            revision: sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
```

`marketplaces` must be non-empty in a v2 document. A binding identity is `name`; duplicate names with different IDs conflict, while several names may intentionally bind to one immutable ID. Each resource records the exact `registry_name` used for display and Marketplace routing, and that binding must resolve to its `registry_id`. A resource identity is `(registry_id, kind, name)`; a duplicate identity with any different binding, acquisition, or integrity value conflicts. Selection edges are keyed by `(registry_id, name, revision)`. Marketplace entries sort by canonical URL then instance ID, bindings by name then ID, resources by identity, and selection edges by their key. The existing 4 MiB, YAML depth/node, duplicate-key, alias/merge, control-character, endpoint, segment, kind, digest, and path bounds apply before normalization.

Every non-collection resource requires `archive_digest`. A collection is mirror-only, requires `collection_manifest_digest` equal to its `content_digest`, and omits `archive_digest`. An index-only non-collection additionally requires `registry_commit`, `registry_manifest_digest`, and `source_path`; those fields are forbidden for mirror resources. The lock always retains the legacy requested `revision` and independently verified `content_digest`. A v2 lock with no Marketplace entry is invalid rather than a second spelling of v1. New readers preserve v2 when Git state is reconciled; old v1 readers reject the version before decoding hosted fields.

Directness and installed aliases are not duplicated into the lock because they are recoverable from v4 request intent. The runtime adapter matches the v4 source and request stream against its owning v2 lock, reconstructs the existing internal Marketplace pin model, and then uses the established resolver, status, revocation, cache, and materialization paths. A missing lock, scope mismatch, endpoint or instance mismatch, unknown registry binding, request/lock revision conflict, dangling selection edge, or conflicting duplicate record fails closed with a stable diagnostic. Git-only locks continue using byte-compatible v1 encoding. Git reconciliation of a v2 lock preserves Marketplace records unchanged; this slice cannot advance or delete them.

Migration is offline. Every legacy resource pin must match verified cache metadata for endpoint instance, registry identity, kind, name, content digest, archive or collection integrity, and index-only occurrence fields where applicable. Missing, stale, corrupt, or inconsistent evidence refuses the selected migration transaction. An endpoint-only legacy entry may transfer its already accepted endpoint/instance binding without inventing a resource record.

All selected writable config roots and their layer locks are read before planning. An absent lock becomes v2; an existing v1 lock is upgraded while preserving all Git records; an existing v2 Marketplace record must match the migrated state exactly. The migrator validates each generated config and lock together through the normal v4 runtime adapter, then publishes every changed config/lock pair through one journaled dependency transaction with generation and read assertions. `--dry-run` performs the same evidence, codec, scope, and adapter checks without publication. Any refusal, concurrent change, staging failure, or interrupted publication leaves the original config and lock bytes authoritative and recoverable.

## 7. Command and editor behavior

The Git/directory parser, adapter, include composition, and representable standalone migration rows below are implemented behavior; the exact-pin Marketplace lock/v2 migration and its read-only adapter are also implemented. Broader hosted Marketplace rows and unsupported migration constructs remain proposed or refused until their acquisition and conversion rules ship.

| Operation | Required behavior |
|---|---|
| `install` into v4 | Bind/copy the source into the destination layer, add the shortest resource item expressing intent, resolve/verify, and transactionally publish config plus lock |
| `remove` from v4 | Remove the targeted explicit request or write a source-qualified exclusion when inherited/dynamic/collection selection still supplies it; preserve other scopes and required selections |
| Marketplace endpoint registration | Add a source declaration only; select no resource and create no incidental installation pin |
| `sync` | Retain compatible exact acquisitions; minimally reconcile new/changed writable requirements; no implicit config migration or unconstrained Marketplace rebinding |
| `sync --locked` | Reject missing/stale required locks; no dependency reconciliation or format migration |
| `sync --check` / `lock --check` | Read-only checks under #123's no-bootstrap/no-recovery/no-network rules |
| `update` / lock upgrade | Advance eligible unpinned resolutions and dynamic memberships; retain explicit exact constraints and immutable identity bindings |
| `update --bump` | Existing Git ref-bump behavior through the config-plus-lock transaction; no implicit release of Marketplace exact revision constraints |
| `list`, `status`, automatic TUI refresh | Use existing exact lock views without advancing dependencies; surface config/lock errors distinctly from output drift |
| `migrate [--to v4] --scope <scope> --dry-run` | Preview the selected layer's representable semantic/selection-preserving config transformation, with no writes, lock changes, or acquisition |
| `migrate [--to v4] --scope <scope>` | Consent-approved or explicit semantic/selection-preserving conversion to canonical v4 YAML and transactional publication; no content upgrade or authored comment/formatting guarantee |

### 7.1 Current implementation status

The repository currently provides the following subset:

| Area | Current state |
|---|---|
| Read, validate, compose, and normalize v4 Git/directory configuration | **Implemented.** The normal loader adapts these sources into the existing runtime requirement model. |
| `list`, `status`, `sync`, `lock`, and `sync --check` for v4 Git/directory sources | **Implemented for the adapted Git/directory path.** Existing lock semantics remain Git-only; directories remain live. |
| `migrate [--to v4]` | **Implemented for representable standalone legacy files, including exact Marketplace pins with verified cache evidence.** The command defaults to v4; startup and TUI consent flows are available; `--scope` and `--dry-run` are supported; unsupported include cases and unproven Marketplace pins fail closed while legacy reads remain available. |
| v4 Marketplace parsing and runtime resolution | **Implemented for the exact-pin slice.** The schema validates Marketplace declarations and bounded selectors; explicit requests hydrate through the read-only adapter when a matching normalized lock/v2 record exists, and missing/mismatched locks or unsupported policy fail closed. |
| v4 `install`/`remove`, shared CLI/TUI editor, and comment-preserving v4 writes | **Remaining.** Current editing behavior is still based on the legacy configuration model. |
| Hosted Marketplace lock acquisition, dynamic-selector snapshots, update, rebind, check, GC, and consent behavior | **Remaining.** The lock/v2 codec and read-only adapter are implemented for migration-produced state; these are the release-blocking hosted boundaries described in sections 6 and 10. |

Removing an exact `revision` constraint is an explicit config edit or future dedicated operation, not an interpretation of ordinary `update`.

CLI and TUI share one resource-request editor and normalization pipeline. They do not independently implement Git and Marketplace selection rules. Editor serialization chooses compact groups and string items where possible, retaining authored expanded forms and comments rather than repeatedly normalizing the entire document on each edit. This editor behavior does not extend to migration, which regenerates canonical v4 YAML without promising authored comments or formatting.

Existing legacy files retain their legacy editing behavior until migration. A legacy operation that cannot represent new functionality returns a migration-required diagnostic. Interactive UX may offer migration, but no rewritten v4 file is published without affirmative consent. Noninteractive execution never migrates incidentally.

## 8. Explicit migration and compatibility

### 8.1 Version coexistence

| Situation | Behavior |
|---|---|
| Existing v2 root or v3 compatibility alias | Accepted through the legacy reader with existing semantics |
| New config root | V4 for the implemented parser/adapter slice; hosted Marketplace remains gated by its lock extension |
| User layer v2, repo layer v4 | Supported; independently normalized and independently locked |
| V4 root containing legacy selection keys | Rejected with migration/field diagnostics |
| Legacy root containing v4 selection keys | Rejected with migration-required guidance |
| Mixed versions inside one include tree | Rejected; no field-shape guessing |
| `sync` on legacy config | No schema rewrite |
| Legacy client on v4 | Explicit unsupported-schema failure |

`migrate` targets v4 by default, while `--to v4` remains an accepted explicit spelling. Interactive startup and the TUI request consent before invoking it; declined or safely refused migration continues through the v2/v3 reader. No deprecation deadline or automatic v2/v3 removal is part of this design.

### 8.2 Semantic and selection-preserving conversion rules

Migration works from authored declarations and selector expressions, not a list of currently materialized resources. This is semantic/selection losslessness, not byte or presentation losslessness: migration regenerates canonical v4 YAML and does not promise to preserve authored comments or formatting.

- Each legacy Git/directory entry becomes a named source plus a resource group. Source names are stable for the preview and committed transformation; generated names must be unique and readable, with deterministic suffixes when needed.
- Legacy per-kind named selections become typed locators. Collections remain collection requests, not enumerated leaves.
- Effective legacy default inheritance, including explicit empty overrides and nil-versus-empty tag behavior, is resolved before translating selectors. Explicit emptiness must not enable implicit all-selection.
- Legacy `required` protection becomes `required: true`. Legacy implicit all-selection becomes `all: true` only when the old resolver would actually auto-select all. A legacy `default: true` with explicit named/tag selection is not mechanically rewritten to all-selection.
- Legacy named exclusions become locators; kind/source `exclude_all` becomes the corresponding bounded exclusion pattern where that preserves behavior.
- Top-level selectors applying to several legacy sources are translated as source-qualified expressions without choosing today's collision winner as the only surviving provider.
- Source roots, requested refs, source layouts, target restrictions, authority, scope, stable collision order, and selection semantics remain intact. A transformation that cannot preserve them is refused.
- Git/directory-only migration does not change lock records. Exact legacy Marketplace migration uses the owning lock only as specified in section 6.4; it performs no branch/tag resolution, Git fetch, Marketplace request, or latest lookup.
- Local directories remain live. Migration must not snapshot them or turn them into lockable Git sources.

The initial automatic migration must refuse include trees or policy combinations for which an equivalent, provenance-preserving transformation has not been implemented and tested. In particular, it must not flatten includes into the root or edit included files merely to complete the conversion. The diagnostic lists the files/constructs requiring a separately reviewed conversion; the legacy config continues to work unchanged.

A failed migration leaves config and lock state unchanged. Git/directory-only migration leaves lockfiles byte-identical. Exact Marketplace migration publishes the selected config/lock pairs through #123's dependency transaction and recovery guarantees. Missing or inconsistent verified cache evidence is diagnosed as a prerequisite rather than acquired implicitly.

### 8.3 Legacy Marketplace exact-pin conversion

The legacy `marketplaces[].resources` format is a migration input, not the intended public v4 authoring surface. The exact-pin slice in section 6.4 converts only state whose intent and acquisition provenance can be reconstructed without network access.

The conversion distinguishes direct requests from collection-derived pins, preserves explicit user revision constraints where representable, and transfers verified identity/acquisition records into the owning lock. It never marks every expanded leaf as a new direct request or treats a saved config digest as fresh verification. Ambiguous pin provenance, dangling collection edges, missing integrity evidence, or an unrepresentable explicit revision is refused without publication.

Legacy-reader support remains an adapter into one internal requirement model, not permission for new editors to write both formats into one file. The exact-pin adapter is read-only from the v4 side; broader Marketplace editing and update behavior remains gated on the complete hosted implementation. Existing v2/v3 organization installations are not rewritten by consumer migration.

## 9. Diagnostics and acceptance criteria

### 9.1 Required diagnostic distinctions

Stable codes must distinguish at least: unsupported schema; legacy/new key mixing; unknown field; invalid source declaration; undefined source name; conflicting included source definitions; invalid locator/options; duplicate resource request; unbounded Marketplace dynamic selector; invalid exclusion pattern; missing/stale/corrupt lock; identity mismatch; incomplete selector enumeration; unsupported semantic migration; and consent required.

Diagnostics identify the owning layer, declaring file, source name, and resource locator where applicable. They do not expose credentials or suggest unsafe mutable fallback. Scope/required/collision diagnostics retain existing behavior rather than being replaced by generic config errors.

### 9.2 Verification matrix

| Area | Required coverage |
|---|---|
| Schema | Every source type, six kinds, both group forms, both item forms, all invalid field/type combinations, duplicates, version boundaries |
| Editing | Comment preservation, minimal diffs, idempotency, one writer shared by CLI/TUI, legacy files unchanged without consent |
| Includes | Additive requests, wildcard exclusions, exact higher-precedence re-inclusion, same-file exclusion, whole-item option replacement, empty versus omitted selector fields, conflicting source bindings |
| Layer isolation | Same-named different sources in user/repo/local, source copying, separate locks, same resource in user and repo, machine exclusion immunity, mandatory protections |
| Identity | Changed instance, slug takeover, permanent old-slug alias, alias-only rename, endpoint migration refusal, missing versus corrupt lock, explicit noninteractive consent |
| Dynamic selectors | Required Marketplace registry scope, any-tag matching, complete enumeration, unchanged-sync stability, intentional update additions/removals, partial-query failure with zero publication/prune |
| Collections | Direct plus collection selection, shared/nested members, removal of one reason, leaf exclusions, exact constraints, conflicting revisions, mirror/index-only mixed members |
| Locks | Git v1 reuse, hosted-version refusal by old readers, no user-wide merged lock, live directories excluded, exact-constraint retention, presentation-only changes not upgrading content |
| Transactions | Failure before publication, split-publication recovery, rollback, concurrent config edits, immutable-cache leases and GC reachability |
| Migration | Positive Git/directory examples plus empty overrides, defaults/tags/required, per-kind excludes, collisions, refused include conversion, deterministic source naming, semantic/selection-preserving content; exact Marketplace mirror/index-only pins, direct and collection-derived selection, aliases, endpoint-only bindings, verified-cache refusal, lock merge, dry-run, and all-layer rollback |
| End to end | Isolated install/remove/update/check/migrate workflows across all writable scopes; no developer-home, live organization, or network dependencies |

Parser/editor/resolver tests must assert independently authored expected results, not compare one production normalization helper with itself. Golden tests cover config edits and generated lock bytes. Relevant hot-path changes need baseline/post-change benchmarks. The implemented parser/adapter, standalone Git/directory migration, and exact Marketplace pin slice are covered by the repository tests, including lock-codec, adapter, cache-evidence, transaction, CLI, and TUI coverage. V4-aware Marketplace editor and broader hosted acceptance coverage remain pending their respective implementation boundaries.

## 10. Integration boundaries and release conditions

The current repository implements the Git/directory parser, include composition, runtime adapter, migration for representable standalone legacy files, and the exact legacy Marketplace migration slice in section 6.4. The shared node-level editors and broader hosted behavior remain implementation boundaries:

1. The Git-only lock foundation from #123 remains available, with v1 bytes unchanged for Git-only state.
2. V4 parsing, the source-bound requirement model, and the legacy normalization adapter with semantic-equivalence tests remain implemented for the Git/directory slice.
3. The v2 lock codec, read-only exact Marketplace adapter, verified-cache evidence reader, and transactional migration described in section 6.4 are implemented without enabling network acquisition or v4 Marketplace edits.
4. Keep shared v4 node-level editors pending; legacy config writes remain supported until Marketplace installs and all CLI/TUI edits route through the common v4 model.
5. Extend the hosted lock and acquisition lifecycle for dynamic selectors, updates, explicit identity rebinding, consent, checks, immutable-cache leases, and garbage collection.
6. Release complete v4 Marketplace consumer functionality only when its config, lock, consent, collection, update, check, and GC paths operate together.

These are the remaining dependency boundaries for complete hosted Marketplace functionality beyond the implemented exact-pin slice.

The current glossary and ADRs describe pins stored in consumer config. Those descriptions remain accurate for v2/v3 config writes; the implemented exact-pin slice moves generated bindings and selection edges into acquisition locks only during migration. The storage consequences in ADRs 0002 and 0005 and the corresponding glossary entries must be explicitly superseded when broader v4-aware Marketplace editors ship. This proposal does not silently rewrite the accepted descriptions of currently implemented behavior.

## Sources

- [PR #123](https://github.com/Adyen/agent-marketplace/pull/123), especially its [layer-owned lock design at the inspected revision](https://github.com/Adyen/agent-marketplace/blob/24123dbf5a8d55a1569094f6dcfc8dbc13685fab/docs/plans/2026-08-31-layer-owned-lockfiles-design.md).
- [Existing config model](../../internal/config/config.go), [Marketplace pins](../../internal/config/marketplace.go), [merge semantics](../../internal/config/merge.go), and [selection implementation](../../internal/resolver/resolver.go).
- [Selection and precedence reference](../PRECEDENCE.md), with implementation and tests authoritative where existing descriptions disagree.
- [Registry occurrence ADR](../adr/0004-separate-registry-occurrences-from-resource-revisions.md).
- [Current domain glossary](../../openspec/config.yaml) and [coding standards](../../CODING_STANDARDS.md).
