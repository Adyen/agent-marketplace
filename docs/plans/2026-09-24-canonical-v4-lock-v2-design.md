# Canonical v4 configuration and lockfile v2 mutation design

**Status:** Implemented

Intentional transition boundaries retained after implementation:

- Requests owned by an included fragment refuse with `config.edit.include-owned` rather than editing the fragment.
- A v4 layer that declares a Marketplace source without endpoint evidence in its lock fails closed (`v4.marketplace-lock-mismatch`); recovery goes through an explicit endpoint install or lock bootstrap that verifies identity online.
- Organization configuration and locks are never written; an intentionally absent organization lock keeps its online transient in-memory acquisition path.
- Hosted dynamic-selector snapshots (`tags`/`all`), hosted rebind, and offline transient acquisition remain outside this design.

**Date:** 2026-09-24

## 1. Problem

`agent-marketplace/v4` is the canonical configuration schema, but the application does not yet treat it as the canonical writable model. V4 roots are parsed and adapted into the legacy `Config` and `RegistryEntry` runtime structures, while most mutation helpers still edit v2/v3 keys such as `registries`, top-level resource selectors, and embedded Marketplace pins. Individual callers have added local v4 branches, so feature support is uneven across CLI install/remove variants, TUI toggles, stale pruning, and Marketplace operations.

The reported failure is representative:

```text
agent-marketplace install daily-sync \
  --from=<git-url> \
  --ref=add-skill-daily-sync \
  --path=teams/unified_platform/plugins/identity_platform/skills/daily-sync \
  --scope=user

v4.writer-opt-in: legacy config writers do not edit v4 files
```

`installFromAdHoc` stages the existing v4 root and then invokes a legacy registry writer before discovery. The guard correctly prevents writing legacy fields into a v4 document, but the command has no common v4-aware mutation boundary to use instead.

Lock handling has the same structural problem. Lockfile v1 is treated as the Git-only format, while v2 is valid only when Marketplace records exist. Git planners therefore start from v1 and every mixed operation must conditionally preserve or upgrade v2. Recent defects have included failure to upgrade an existing v1 lock when adding a Marketplace source, accidental loss of Marketplace evidence during Git reconciliation, and permission changes while rewriting locks.

## 2. Goals

1. Every feature available with v2/v3 configuration works with v4 configuration.
2. CLI, TUI, repair, and stale-prune callers express semantic mutations without inspecting the configuration schema.
3. V4 is the only configuration format created for new writable roots.
4. Lockfile v2 is the only lock format emitted by writers, including Git-only locks.
5. Existing v2/v3 configs and v1 locks remain readable during a deliberate deprecation period.
6. Config and lock changes publish as one validated, recoverable transaction.
7. Authored comments, ordering, permissions, include provenance, unrelated selections, and unrelated lock evidence survive edits.
8. Ambiguous, malformed, unsupported, stale, or concurrently changed inputs fail closed without partial publication.

## 3. Non-goals

1. Hosted dynamic-selector snapshots for Marketplace `tags` or `all`, because these were not supported by v2 and are not required for parity.
2. Silent migration of authored v2/v3 configuration during an unrelated command.
3. Editing organization-managed configuration or locks.
4. Changing resolver precedence, required-resource immunity, materialization policy, harness targeting, or collision policy.
5. Flattening include trees or editing included fragments implicitly.
6. Retaining lockfile v1 as a write target.

## 4. Root cause

The incompatibilities come from three independent version boundaries.

### 4.1 Runtime boundary

V4 is converted into the legacy runtime shape. That adapter makes existing readers and the resolver work, but it also lets downstream code assume a legacy registry entry is the writable declaration. Information such as the v4 source name and the distinction between a root declaration and included declarations is not available from a plain legacy mutation helper.

### 4.2 Editor boundary

The config package exposes many operation-specific legacy helpers. Callers compose several helpers to implement one semantic operation, often with intermediate reads and writes. V4 parity has consequently been added per call site rather than per mutation. A path fixed for a TUI toggle does not automatically fix configured install, ad-hoc install, remove, collection handling, or stale pruning.

### 4.3 Lock boundary

Lockfile v2 is modeled as a conditional extension of v1 rather than its successor. `lockfile.Plan` creates v1 for Git requirements; the codec rejects v2 documents without Marketplace records. Every mixed operation must preserve the old version and Marketplace records manually. Missing one preservation branch can downgrade a lock or discard evidence.

## 5. Considered approaches

### 5.1 Patch each call site

Port the open v4 parity implementation and continue adding `if v4` branches to install, remove, TUI, and lock paths.

This is the fastest way to fix individual commands, but it retains two public writer families and distributed version decisions. New features remain vulnerable to the same regressions. This approach is rejected.

### 5.2 Force migration before every mutation

Support only v4 writers and require all v2/v3 roots to migrate before install/remove/toggle operations.

This is the cleanest final state, but it immediately breaks noninteractive automation and configurations whose include trees or Marketplace state cannot yet be migrated automatically. This is the intended end state, not the initial transition.

### 5.3 One semantic editor with temporary serialization backends

All callers use one version-neutral mutation engine. It has a v4 backend and a temporary legacy backend, both governed by the same operation semantics and contract tests. New roots use v4. Legacy roots remain writable with a deprecation warning during the transition, then become migration-required in a later removal release.

This approach is selected.

## 6. Prior art

### 6.1 PR #296: v4 resource editing parity

PR #296 implemented many required behaviors and provided useful regression coverage for Git/directory install and remove variants, stale pruning, exact Marketplace editing, TUI toggles, and config/lock publication.

It was not the selected architecture because it added schema branches throughout callers and hundreds of lines of operation-specific free editor functions. It targeted the closed PR #291 branch rather than `main`, and automated review identified unresolved risks involving duplicate insertion, a nil dereference during v4 lock pruning, and structural comparison of slice-bearing records.

This implementation reused independently valid behavior contracts and test scenarios from PR #296 rather than its call-site branching structure.

### 6.2 PRs #293 and #302

PR #293 hardened lockless organization Marketplace doctor/GC behavior. PR #302 narrowed transient organization acquisition cache invalidation after selection edits. Both were orthogonal to the editor interface but overlapped runtime-lock and read-assertion code.

The transaction boundary preserves:

- transient in-memory organization evidence without publishing an absent organization lock;
- fail-closed doctor/GC behavior;
- organization-config and lock-absence read assertions;
- target-harness and allowlist fingerprints;
- revalidation before destructive cache or repair operations.

## 7. Layer editor

### 7.1 Public domain types

The config package exposes a cohesive editor rather than operation-specific schema helpers.

```go
type LayerTarget struct {
    Source        Source
    StagedPath    string
    DeclaringPath string
}

type SourceBinding struct {
    Name  string
    Entry RegistryEntry
}

type ResourceLocator struct {
    Kind ir.ResourceKind
    Name string
}

type SelectionOperation uint8

const (
    SelectionInclude SelectionOperation = iota
    SelectionExclude
    SelectionRemoveInclude
    SelectionClearExclude
    SelectionSelectAll
    SelectionExcludeAll
    SelectionAddTag
    SelectionRemoveTag
)

type SelectionChange struct {
    Operation SelectionOperation
    Locators  []ResourceLocator
    Tag       string
}
```

`SourceBinding.Name` is the v4 source name and is empty for a legacy root. `SourceBinding.Entry` is the normalized physical identity used for catalog and lock matching. Directory bindings normalize to the resolved bundle root; Git bindings preserve URL/ref/path/source-harness identity.

### 7.2 Session API

Conceptually:

```go
editor, err := OpenLayerEditor(target)
binding, err := editor.EnsureRegistry(entry)
binding, err := editor.EnsureMarketplace(endpoint)
err = editor.Apply(binding, change)
err = editor.RemoveSource(binding)
candidate, err := editor.Render()
```

The exact Go shape may use methods or a closed intent type, but it must preserve these boundaries:

- schema detection happens once when the editor opens;
- source identity is resolved once and returned as an opaque binding;
- callers cannot address a source by v2 keys or v4 names directly after binding;
- multiple semantic changes apply in memory;
- validation and serialization happen once;
- the editor returns candidate bytes and metadata without publishing them.

### 7.3 Backends

The v4 and temporary legacy backends are private. They implement document-location and serialization details only. Semantic decisions—when an exclusion is necessary, whether an include should be removed, and what operation a user action means—remain above the backends.

The v4 backend:

- matches Git by normalized URL/ref/path/source-harness;
- matches directories by resolved physical bundle identity while retaining authored safe relative paths where possible;
- matches Marketplace by canonical endpoint;
- generates deterministic unique source names;
- adds root overrides instead of modifying included files;
- retains compact groups/items when possible and expands only when required;
- validates the candidate against the original declaring path and layer.

The legacy backend:

- preserves existing v2/v3 behavior during the transition;
- is reachable only through `LayerEditor`;
- emits `config.legacy-deprecated` once per command;
- does not gain new standalone public helper entry points.

### 7.4 Include policy

`StagedPath` and `DeclaringPath` are intentionally separate. Staging may place the writable root in a temporary directory, but includes, directory-source bases, and layer validation must resolve from the real declaring root.

Included fragments are read to establish source identity and precedence. Mutations write only the selected root. If a safe root override cannot represent the operation without ambiguity or semantic loss, the editor returns a coded error and writes nothing.

## 8. Layer mutation transaction

`pipeline.MutateLayer` is the only publication boundary for resource/source mutations.

Conceptually:

```go
err := pipeline.MutateLayer(ctx, target, func(m *LayerMutation) error {
    binding, err := m.EnsureRegistry(entry)
    if err != nil {
        return err
    }
    return m.Apply(binding, change)
})
```

The operation:

1. recovers prior dependency transactions;
2. snapshots the config root and owning lock generation/mode;
3. stages the config root;
4. opens `LayerEditor` with staged and declaring paths;
5. applies semantic operations;
6. derives Git and Marketplace requirements from the candidate;
7. acquires or reuses verified evidence as policy allows;
8. creates a canonical lockfile v2 candidate;
9. validates the config/lock pair through the normal layer loader;
10. builds read assertions for all policy and acquisition inputs;
11. publishes config and lock through `dependencytxn` using compare-and-swap generations;
12. runs requested sync/materialization work;
13. performs guarded rollback or journal recovery if post-publication work fails;
14. removes staging residue.

Pure AST editing remains in `internal/config`. Network access, cache evidence, trust policy, lock planning, publication, and rollback remain in `internal/pipeline` and `internal/dependencytxn`.

## 9. Canonical lockfile v2

### 9.1 Schema policy

Lockfile v2 becomes valid for all combinations:

- Git-only;
- Marketplace-only;
- mixed Git and Marketplace;
- empty generated state when retaining an empty lock is required by an owning transaction.

Marketplace record validation remains strict when records are present.

### 9.2 Writer policy

- Every new lock is v2.
- Every planner starts with a v2 desired document.
- Every rewrite of a writable v1 lock upgrades it to v2 while preserving Git records, file mode, and generation checks.
- No helper conditionally downgrades or preserves v1 as desired output.
- Pruning the last Git or Marketplace record retains a valid v2 document when the owning layer still requires a lock, or removes it only through an explicit lifecycle decision.

### 9.3 Reader policy

- V1 remains readable during the transition.
- Writable v1 emits `lock.version-deprecated` and upgrades on the next reconciliation.
- `lock --check` reports the required upgrade without writing.
- `--locked` may consume v1 during the transition but reports the deprecation.
- Organization v1 remains readable and is never rewritten by a consumer.

Repositories that commit a Git-only v2 lock establish a new minimum client version. Older clients currently reject v2 without Marketplace records; that refusal is fail-closed and must be documented as a deliberate compatibility boundary.

## 10. V2 feature-parity matrix

| Existing operation | V4 config behavior | Lock v2 behavior |
|---|---|---|
| Add Git source | Ensure named Git source | Add/reconcile exact Git record |
| Add directory source | Ensure named directory source | No lock record |
| Install named resource | Add typed locator and clear exact exclusion | Retain/reconcile source inventory |
| Install collection | Add collection locator | Retain Git inventory or exact Marketplace selection graph |
| Install by tag | Add source-bound tag | Reconcile source inventory |
| Configured `--all` | Add explicit discovered locators, preserving snapshot semantics | Reconcile source inventory |
| Ad-hoc bundle | Set source-bound `all: true` | Reconcile source inventory |
| Remove direct selection | Remove include; add exclusion only when policy may reselect | Retain source inventory |
| Remove collection member | Add exact leaf exclusion | Retain collection evidence |
| Remove source | Remove source and root resource group | Prune unreferenced source records |
| Exclude inherited source | Ensure shadow binding and set source exclusion | Retain lower-layer evidence |
| Re-enable | Remove exact exclusion and add include | Retain/reconcile source inventory |
| TUI toggle | Use the same semantic operation as CLI | Use the same transaction as CLI |
| Collision exclusion | Add exact source-bound exclusion | Preserve unrelated evidence |
| Stale prune | Remove direct root include; refuse ambiguous include-owned cleanup | Prune only unreferenced evidence |
| Register Marketplace endpoint | Ensure Marketplace source | Store verified endpoint identity |
| Install Marketplace request | Add exact request/options | Store revision, registry binding, integrity, and selection graph |
| Remove Marketplace request | Remove request/options | Prune evidence no retained request needs |
| Marketplace update | Preserve request unless an explicit constraint changes | Replace verified resolution atomically |
| Git update | Preserve config unless an explicit ref bump is requested | Replace selected Git records |
| Harness/telemetry edit | Continue schema-neutral field edit | No lock change |

Hosted Marketplace dynamic tag/all snapshots remain outside parity because v2 did not support them.

## 11. Marketplace update semantics

V2 stored requested and resolved Marketplace state together in config pins. V4 separates durable intent from generated acquisition evidence.

- An unconstrained exact request updates by replacing its lock record with the newest verified policy-allowed revision. Config bytes remain unchanged.
- A request carrying an explicit revision constraint remains pinned during ordinary update.
- A collection update atomically replaces the collection lock record, member records, and selection edges while retaining the config request.
- Removing one selection reason does not remove a resource retained by another direct or collection reason.
- Endpoint instance or registry identity changes fail closed. Rebinding requires a separate explicit operation and user-visible consent.
- Update never derives trust from stale config bytes or rewrites lock evidence without re-verification.

## 12. Deprecation

### 12.1 Transition release

- `init` and every absent writable user/repo/local root create v4.
- V2/v3 roots remain readable and writable only through the legacy `LayerEditor` backend.
- Mutations emit `config.legacy-deprecated` with an exact scope-specific migration command.
- `doctor` and `status` report legacy writable roots.
- Missing and existing writable v1 locks become v2 on reconciliation.
- Direct caller use of legacy mutation helpers is prohibited by tests/static checks.

### 12.2 Removal release

- V2/v3 mutations return `config.migration-required`.
- Reads and explicit migration remain available.
- The legacy editor backend and old public mutation helpers are removed.
- V1 lock reads may remain for externally managed compatibility, but writable consumers never produce or preserve v1.

No unrelated command silently migrates authored config. Existing explicit migration consent and fail-closed migration rules remain authoritative. The removal release is not part of this implementation: it requires a separate approved change after at least one release has emitted deprecation warnings, first-party configs and fixtures have migrated, and the migrator can represent every still-supported v2/v3 mutation shape.

## 13. Diagnostics

Stable diagnostics must distinguish:

- `config.legacy-deprecated`;
- `config.migration-required`;
- `config.edit.ambiguous-source`;
- `config.edit.unsupported-operation`;
- `config.edit.unsafe-directory`;
- `config.edit.include-owned` when a safe root override cannot represent the mutation;
- `lock.version-deprecated`;
- `lock.version-upgrade-required` in check-only mode;
- the existing config/lock scope and identity mismatch codes;
- the existing missing, malformed, stale, or unverifiable Marketplace evidence codes;
- `dependency.generation-conflict`;
- the existing interrupted-publication recovery diagnostics.

Callers branch on typed errors or codes, never message text. Diagnostics include scope, declaring root, source identity, and locator where safe, without credentials or sensitive payloads.

## 14. Verification

### 14.1 Editor contract tests

A table drives the same semantic operations against v2, v3, and v4 fixtures. Assertions compare independently authored effective intent and externally relevant bytes, not one production adapter against another.

Coverage includes:

- every resource kind and collection;
- Git and directory source identity;
- include/exclude/remove-include/clear-exclude/all/tag operations;
- comments, ordering, compact/expanded forms, modes, idempotency, and mtime no-ops;
- root plus include precedence;
- ambiguity, wildcard conflicts, traversal, and symlink escape;
- legacy deprecation diagnostics.

### 14.2 Lock contract tests

A matrix covers missing/v1/v2 inputs crossed with Git-only, Marketplace-only, and mixed requirements. Every writable result is v2 and preserves unrelated records and permissions. Organization read-only behavior is covered separately.

### 14.3 Transaction tests

Failure injection covers:

- editor validation before publication;
- acquisition/evidence failure;
- config or lock generation conflict;
- policy input changing after preview;
- split publication and recovery;
- post-publication sync failure and guarded rollback;
- staging cleanup and absence of residue.

### 14.4 CLI/TUI and E2E tests

- Reproduce the reported `install --from --ref --path --scope=user` command.
- Cover configured/ad-hoc named, tag, all, bundle, and collection install.
- Cover direct, collection-member, cross-layer, source, and stale removal.
- Cover user/repo/local scopes and every resource kind.
- Cover exact Marketplace endpoint/install/remove/toggle/update behavior.
- Prove CLI and TUI produce the same semantic mutation.
- Keep all tests isolated from the developer home and external network.

### 14.5 Performance and final gates

Editor open/apply/render and lock planning are sync/install hot paths and require retained benchmarks with before/after measurements. Final verification includes focused package tests during each slice and one final full `make test`, `make ci-lint`, `make build`, E2E, and relevant TUI QA pass.

## 15. Implementation slices

1. **Universal lock v2**
   - relax v2 Git-only validation;
   - make planners and reconcilers emit v2;
   - add v1 read/v2 write diagnostics and tests.

2. **Layer editor**
   - add semantic types, source bindings, editor session, v4 backend, temporary legacy backend, and contract tests;
   - keep publication out of config.

3. **Layer mutation transaction**
   - stage config and lock;
   - derive requirements and evidence;
   - validate and publish one candidate pair.

4. **Git/directory migration**
   - route install, remove, TUI, collision, and stale-prune paths through the common transaction;
   - remove caller schema branches and direct legacy-helper calls.

5. **Marketplace migration**
   - route endpoint, exact request, collection, toggle, remove, and update operations through the common transaction;
   - preserve transient organization acquisition and doctor/GC safety contracts.

6. **Deprecation and cleanup**
   - create only v4 roots;
   - add warnings and migration guidance;
   - add enforcement against new legacy-helper use;
   - update published docs and project guidance;
   - prepare the later removal release.

Each slice starts with failing regressions and lands only after its focused tests pass. The complete project gate runs once after all slices are integrated.

## 16. Success criteria

- The original command succeeds against a v4 user config and materializes the requested resource.
- Every v2 mutation feature has a tested v4 equivalent.
- CLI/TUI/pipeline callers contain no schema-version branches for resource/source mutation.
- New writable configs are v4 and new/rewritten locks are v2.
- V1 lock upgrades cannot discard Git or Marketplace records or alter existing permissions.
- Legacy config behavior remains compatible during transition and emits actionable deprecation diagnostics.
- No failed operation changes config, lock, materialized output, or leaves staging residue outside explicitly documented post-publication recovery behavior.
