# Unified resource configuration design

**Status:** Proposed specification; not implemented. The core schema and migration choices were agreed in the design discussion. Detailed validation and implementation contracts below are proposals for review.

**Prerequisite:** [PR #123: layer-owned Git lockfiles](https://github.com/Adyen/agent-marketplace/pull/123) merges before Marketplace consumer functionality. The lockfile baseline inspected for this design is commit `24123dbf5a8d55a1569094f6dcfc8dbc13685fab`.

**Audience:** Client implementers and reviewers. This document fixes the proposed consumer config v4 syntax and specifies its required relationship with acquisition locks. It is not documentation of currently supported CLI behavior.

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
| Migration | Explicit opt-in to `agent-marketplace/v4`; existing v2/v3 configs remain supported |

This design does not change harness configuration precedence, source allowlist authority, materialization modes, aggregate ownership, trust handling, resource prerequisites, or existing collision policy. It does not introduce dependency installation or semantic-version ranges.

The hosted lock extension is a prerequisite for shipping Marketplace selections in this schema. Moving pin fields out of config without that extension is not a valid intermediate release.

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

Unknown keys, duplicate mapping keys, wrong node types, multiple YAML documents, and malformed resource items are errors. YAML merge keys or aliases must not provide an alternative include or override mechanism. Config serialization remains comment-preserving and node-based.

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

`required` is selection protection, not a selection expression: `required: true` alone selects nothing in v4. Legacy implicit all-selection is translated to explicit `all: true` during a lossless migration. Required selections remain immune to exclusions and consumer removal under the existing authority rules.

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

Config version and lock version are independent. Git-only v4 requirements can reuse valid `agent-marketplace-lock/v1` records without a format-only rewrite. The hosted extension must use a new, explicitly versioned lock format, proposed as `agent-marketplace-lock/v2`, because v1 rejects unknown fields. New clients retain v1 reading; old clients must fail on the new version rather than dropping Marketplace state.

This document fixes the config wire schema, not the complete hosted lock codec. The following record content and behavior are mandatory inputs to that codec's design. Its precise field layout, canonical encoding, bounds, and golden fixtures must be specified and reviewed before the hosted extension is implemented or released.

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

## 7. Command and editor behavior

These are required behaviors, not claims that the current CLI implements v4. New migration flags shown below are proposed syntax.

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
| `migrate --to v4 --scope <scope> --dry-run` | Preview the selected layer's representable config transformation and lock reuse, with no writes or acquisition |
| `migrate --to v4 --scope <scope>` | Explicit lossless conversion and transactional publication; no content upgrade |

Removing an exact `revision` constraint is an explicit config edit or future dedicated operation, not an interpretation of ordinary `update`.

CLI and TUI share one resource-request editor and normalization pipeline. They do not independently implement Git and Marketplace selection rules. Serialization chooses compact groups and string items where possible, retaining authored expanded forms and comments rather than repeatedly normalizing the entire document on each edit.

Existing legacy files retain their legacy editing behavior until migration. A legacy operation that cannot represent new functionality returns a migration-required diagnostic. Interactive UX may offer migration, but no rewritten v4 file is published without affirmative consent. Noninteractive execution never migrates incidentally.

## 8. Explicit migration and compatibility

### 8.1 Version coexistence

| Situation | Behavior |
|---|---|
| Existing v2 root or v3 compatibility alias | Accepted through the legacy reader with existing semantics |
| New config root | V4 after the compatibility implementation ships |
| User layer v2, repo layer v4 | Supported; independently normalized and independently locked |
| V4 root containing legacy selection keys | Rejected with migration/field diagnostics |
| Legacy root containing v4 selection keys | Rejected with migration-required guidance |
| Mixed versions inside one include tree | Rejected; no field-shape guessing |
| `sync` on legacy config | No schema rewrite |
| Legacy client on v4 | Explicit unsupported-schema failure |

The existing default behavior of `migrate` is not silently repurposed. The new format migration is named explicitly through `--to v4`. No deprecation deadline or automatic v2/v3 removal is part of this design.

### 8.2 Lossless conversion rules

Migration works from authored declarations and selector expressions, not a list of currently materialized resources.

- Each legacy Git/directory entry becomes a named source plus a resource group. Source names are stable for the preview and committed transformation; generated names must be unique and readable, with deterministic suffixes when needed.
- Legacy per-kind named selections become typed locators. Collections remain collection requests, not enumerated leaves.
- Effective legacy default inheritance, including explicit empty overrides and nil-versus-empty tag behavior, is resolved before translating selectors. Explicit emptiness must not enable implicit all-selection.
- Legacy `required` protection becomes `required: true`. Legacy implicit all-selection becomes `all: true` only when the old resolver would actually auto-select all. A legacy `default: true` with explicit named/tag selection is not mechanically rewritten to all-selection.
- Legacy named exclusions become locators; kind/source `exclude_all` becomes the corresponding bounded exclusion pattern where that preserves behavior.
- Top-level selectors applying to several legacy sources are translated as source-qualified expressions without choosing today's collision winner as the only surviving provider.
- Source roots, requested refs, source layouts, target restrictions, authority, scope, stable collision order, and comments remain intact. A transformation that cannot preserve them is refused.
- Existing Git lock records are matched by normalized acquisition requirements and reused byte-for-byte where possible. No branch/tag resolution, Git fetch, or Marketplace latest lookup is part of a format-only migration.
- Local directories remain live. Migration must not snapshot them or turn them into lockable Git sources.

The initial automatic migration must refuse include trees or policy combinations for which an equivalent, provenance-preserving transformation has not been implemented and tested. In particular, it must not flatten includes into the root or edit included files merely to complete the conversion. The diagnostic lists the files/constructs requiring a separately reviewed conversion; the legacy config continues to work unchanged.

A failed migration leaves config and lock unchanged. Config and required lock-format changes use #123's dependency transaction and recovery guarantees. A missing/stale lock that would require acquisition is diagnosed as a prerequisite rather than resolved implicitly by migration.

### 8.3 Unreleased Marketplace pin format

The existing branch's `marketplaces[].resources` format is not the intended public migration path. Marketplace consumer functionality should ship directly on v4 and the hosted lock extension.

If development installations need conversion, the preview must distinguish direct requests from collection-derived pins, preserve explicit user revision constraints where provenance proves them, and transfer verified identity/acquisition records into the owning lock. It must not mark every expanded leaf as a new direct request or treat a saved digest as fresh verification. Ambiguous pin provenance requires confirmation or manual remediation.

Legacy-reader support is an adapter into one internal requirement model, not permission for new editors to write both formats into one file. This design does not require rewriting existing v2/v3 organization installations merely to install the newer binary.

## 9. Diagnostics and acceptance criteria

### 9.1 Required diagnostic distinctions

Stable codes must distinguish at least: unsupported schema; legacy/new key mixing; unknown field; invalid source declaration; undefined source name; conflicting included source definitions; invalid locator/options; duplicate resource request; unbounded Marketplace dynamic selector; invalid exclusion pattern; missing/stale/corrupt lock; identity mismatch; incomplete selector enumeration; unsupported lossless migration; and consent required.

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
| Migration | Positive legacy examples plus empty overrides, defaults/tags/required, per-kind excludes, collisions, refused include conversion, deterministic alias generation, byte-preserved content pins |
| End to end | Isolated install/remove/update/check/migrate workflows across all writable scopes; no developer-home or live organization dependencies |

Parser/editor/resolver tests must assert independently authored expected results, not compare one production normalization helper with itself. Golden tests cover config edits and generated lock bytes. Relevant hot-path changes need baseline/post-change benchmarks. Documentation-only adoption of this proposal does not imply these implementation tests have passed.

## 10. Integration boundaries and release conditions

Implementation depends on these boundaries, in order:

1. Merge #123 without broadening its Git-only scope.
2. Add v4 parsing, a source-bound requirement model, and a legacy normalization adapter with semantic-equivalence tests.
3. Add shared node-level editors and explicit, lossless migration. Legacy config writes remain supported.
4. Specify and implement the versioned hosted lock codec, identity bindings, selector snapshots, and acquisition adapters on #123's transaction/lease lifecycle.
5. Route Marketplace installs and all CLI/TUI config edits through the common model; remove generated pin bookkeeping from new config writes.
6. Release Marketplace consumer functionality only when its config, lock, consent, collection, update, check, and GC paths operate together.

These are dependency boundaries, not tracked execution state. No implementation, commit, push, or feature activation is authorized merely by the existence of this document.

The current glossary and ADRs describe pins stored in consumer config. During implementation, the storage consequences in ADRs 0002 and 0005 and the corresponding glossary entries must be explicitly superseded: identity/occurrence/collection semantics remain, but generated bindings and selection edges move to acquisition locks. This proposal does not silently rewrite the accepted descriptions of currently implemented behavior.

## Sources

- [PR #123](https://github.com/Adyen/agent-marketplace/pull/123), especially its [layer-owned lock design at the inspected revision](https://github.com/Adyen/agent-marketplace/blob/24123dbf5a8d55a1569094f6dcfc8dbc13685fab/docs/plans/2026-08-31-layer-owned-lockfiles-design.md).
- [Existing config model](../../internal/config/config.go), [Marketplace pins](../../internal/config/marketplace.go), [merge semantics](../../internal/config/merge.go), and [selection implementation](../../internal/resolver/resolver.go).
- [Selection and precedence reference](../PRECEDENCE.md), with implementation and tests authoritative where existing descriptions disagree.
- [Marketplace identity ADR](../adr/0002-separate-marketplace-identity-from-installed-names.md).
- [Registry occurrence ADR](../adr/0004-separate-registry-occurrences-from-resource-revisions.md).
- [Collection membership ADR](../adr/0005-use-content-addressed-collection-manifests-and-selection-edges.md).
- [Current domain glossary](../../CONTEXT.md) and [coding standards](../../CODING_STANDARDS.md).
