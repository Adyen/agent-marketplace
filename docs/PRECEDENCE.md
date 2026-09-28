# Selection and precedence reference

This reference describes how `agent-marketplace` decides which resources are enabled, which source provides each one, and which exclusions and removals win. It is written for contributors and for people who author organization, user, or repository configuration.

The same rules apply to all five kinds that are written into harness folders: skills, commands, subagents, hooks, and MCP servers. Collections are bundles you can select from a catalog; they are never written into a harness folder themselves.

## Schema requirement

V4 (`apiVersion: agent-marketplace/v4`) is the canonical writable schema. It uses typed `sources` and `resources` keys, include trees within each layer, and lock v2 evidence. Every new config root is v4, including roots that install, remove, and TUI edits create on demand.

The v2 schema (`apiVersion: agent-marketplace/v2`) and its `agent-marketplace/v3` compatibility alias are deprecated. Every v2/v3 layer still loads through the v2/v3 reader. Mutations of a v2/v3 layer still run through the legacy editor backend, which emits a `config.legacy-deprecated` warning naming the migrate command for that scope.

The `apiVersion` rules depend on the kind of document:

| Document | `apiVersion` rule |
|---|---|
| v4 root | Must declare `apiVersion: agent-marketplace/v4` (`v4.api-version-required` when missing, `v4.api-version` when different). |
| v4 included fragment | May omit the version. An explicit v2/v3 version is rejected, and so is any legacy installation key (`v4.legacy-key`). |
| Regular legacy root | Must declare v2 or v3. A root without `apiVersion` (v1) is rejected. |
| Generated legacy `config.local.yaml` root | May omit `apiVersion`. If present, it must be v2 or v3. |
| Legacy included fragment | May omit `apiVersion`. |

Normal CLI readers recognize v4 and adapt each layer into the runtime requirement model. V4 sources and resource groups are composed within their own root and include tree before the normal layer order applies. V4 does not make the legacy `registries`, `skills`, or `marketplaces` keys valid.

### Migration consent

`agent-marketplace migrate` targets v4 by default; `--to v4` remains an accepted compatibility spelling.

- Interactive CLI startup and the TUI ask before migrating only the user-personal v2/v3 root (`~/.agents/config.yaml`).
- Repository and personal-local roots are never offered for startup migration. Migrate them explicitly with `agent-marketplace migrate --scope repo` or `--scope local` (or `--scope all`).
- A declined or non-interactive prompt leaves the files unchanged, and the run continues with the v2/v3 reader.
- Use `agent-marketplace migrate --to v2` only for the legacy structural-remediation flow.

## Config includes

All four config layers accept `include` as one local path or an ordered list of local paths:

```yaml
apiVersion: agent-marketplace/v4
include: profiles/company.yaml
```

Use a sequence when order matters:

```yaml
include:
  - profiles/company.yaml
  - profiles/payments-team.yaml
```

### Path resolution and repeated files

- Relative paths resolve from the directory of the file that declares them. Absolute paths are accepted.
- Includes are recursive.
- Glob patterns and remote URLs are not supported.
- Legacy include trees identify files by canonical path. When the same file is reached more than once, including through a symlink or a normalized relative path, it contributes only at its first position.
- V4 include trees keep branch-local ancestry instead. A shared fragment expands once per include branch, and only an ancestor repeated on the current branch is a cycle.

### Organization include trust

Organization includes have an extra trust requirement, whether or not Git registries or an organization lock are present. External relative and absolute paths remain supported, but:

- Every included file must be a root-owned regular file with no group or other write permission.
- Every lexical ancestor directory up to the filesystem root must be root-owned, not a symlink, and not group- or other-writable.
- Validation runs before symlink resolution and deduplication, so path normalization cannot bypass a symlink or writable directory that appears before `..`.
- An unsafe fragment fails the organization layer with an `organization.trust.*` diagnostic.
- System symlink aliases such as macOS `/etc`, `/var`, and `/tmp` are not exceptions for included paths. Use trusted physical paths such as `/private/etc` instead.

User, repository, and personal-local includes do not have this requirement.

### Merge order within one layer

Each included fragment has lower precedence than the file containing it:

- In a list, the first fragment merges first, later fragments merge over earlier ones, and the containing file merges last. Recursive includes follow the same rule.
- Merging keeps its field-specific rules. `include` does not turn every field into a simple replacement.
- For top-level collection selectors and local resource selectors, later files decide conflicts for the same name.
- Within one file, `exclude` wins over `include`.
- Once any contributing file sets `exclude_all: true`, it stays true.

Loading the owning layer fails if any include is missing, unreadable, malformed, invalid for that layer, or part of a direct or indirect cycle. Included fragments may omit `apiVersion` as described in [Schema requirement](#schema-requirement); that exception does not relax validation for a regular root.

### Editing a layer that has includes

The CLI reads included content when it calculates the effective configuration, but config-editing commands modify only the selected root. They never rewrite included fragments.

- Removing a named resource or collection that an include owns records an exclusion in the selected root.
- Whole-registry `remove --from` writes a registry-level `exclude_all` shadow in the selected root.
- The included file stays unchanged, and normal scope boundaries still apply.

### Legacy registry `path` aliases

In a legacy registry entry, when `url` is omitted, `path` is the location alias (for example, `- path: /opt/agent-skills`). When `url` is present, `path` names a subdirectory. Config editors and removals keep this alias-versus-subdirectory interpretation, so use the authored URL/path spelling when targeting an entry.

Resolver matching is separate. Equivalent registry-path spellings such as `./foo/`, `foo/.`, repeated or mixed separators, and the root aliases `.` and empty all canonicalize to the same path identity. This does not rewrite the authored config.

## Explicit v4 migration safety

`agent-marketplace migrate` (or `agent-marketplace migrate --to v4`) converts the format only. It does not upgrade selections.

- Without `--scope`, it validates the writable user, repository, and repository-personal roots as one transaction. `--scope user|repo|local` limits validation and publication to that layer.
- It never reads or writes organization configuration.
- Each migrated config gets an adjacent private backup named `<config>.pre-v4.<UTC timestamp>.bak` containing its exact legacy bytes.
- The backup, the v4 replacement, and any owning layer lock that exact Marketplace pins require (created or upgraded to `agent-marketplace-lock/v2`) are published together in one journaled dependency transaction. Git/directory-only conversion leaves generated locks unchanged.
- `--dry-run` performs the same representability and v4-reader checks without writing a backup, config, or lock.
- New user configs created by `agent-marketplace init` use v4 directly.

### Refusals

Conversion refuses and changes nothing rather than guess. The migrator refuses:

- A legacy include tree, because flattening it or editing its fragments would change provenance.
- Legacy hosted Marketplace selections whose pin provenance cannot be proven from verified local cache evidence.
- Collection-derived aliases that cannot be represented without creating new direct intent.
- Top-level (source-free) kind selectors in a personal-local config.
- A repository-relative local registry in the user config.
- Any other source, selector, revision, or policy that cannot be represented without semantic loss.

A refusal or validation failure publishes none of the selected files, and the legacy configuration stays usable and unchanged.

### Reading v4 roots

Normal CLI discovery, status, and synchronization read v4 roots for Git, directory, and explicit Marketplace sources.

- Source kinds are explicit: use `git`, `directory`, or `marketplace`. A live directory is not inferred from a `.git` suffix.
- Directory sources resolve relative to the declaring config file, stay live and unlocked, and do not accept the Git-only `ref` or `path` fields.
- Marketplace sources normally resolve through a matching normalized `agent-marketplace-lock/v2` record in the owning layer's lock. Writing commands generate missing writable-layer locks.

When the organization lock is intentionally absent:

- An online consumer run that is not a check resolves explicit requests into a verified in-memory lock. It validates effective Marketplace and registry policy before network acquisition. Before use, it revalidates all configuration reads (including the effective target harnesses and the registry and Marketplace allowlists) and confirms the lock is still absent. It never publishes organization policy.
- Online `sync --check` also resolves explicit requests into verified transient evidence without publishing the organization lock.
- Offline runs and `lock --check` still require deployed evidence and report `v4.marketplace-lock-required`.

A present lock that is malformed, untrusted, or mismatched stays exact and fails closed without transient fallback (`v4.marketplace-lock-mismatch`). Dynamic selectors remain unsupported. Reading configuration intent never acquires a lock, and cache garbage collection performs normal work without an organization lock.

### Editing v4 roots

All selection editors work on v4 roots:

- Git/directory install, remove, and TUI toggles and prune edit v4 source bindings and resource groups.
- Exact Marketplace endpoint install, resource install and remove, and update edit v4 `marketplace` sources and `resources` requests, and merge verified evidence into the owning layer's lock v2 record.
- Each semantic mutation publishes the config edit and the lock edit atomically in one journaled dependency transaction, so a failure or generation conflict changes neither file.
- A request owned by an included fragment is refused with `config.edit.include-owned`; the fragment is never rewritten.

Legacy configuration writers return `v4.writer-opt-in` instead of rewriting v4 roots. There are two v4-aware exceptions:

1. The TUI harness edit (`SetHarnessesInConfig`) replaces the top-level `harnesses` list in place. The field is identical in v2/v3 and v4. The edit validates the encoded document through the normal root-document rules (v4 unknown-field and layer-field rules; v2/v3 strict decoding) before anything is written, so a refused edit changes no bytes. It never consults Marketplace lock availability, because an acquisition lock is runtime state and not a property of the document.
2. The TUI user override of an optional organization Marketplace pin (`SetUserMarketplaceExclusion`) adds or removes only the exact `resources.<source>.exclude` locator in the v4 user root.

### Marketplace exclusions

- A Marketplace source whose group contains only exclusions acquires nothing, so it needs no lock and never gets a lock record. A stale record for such a source is ignored until the next lock refresh drops it.
- Marketplace exclusions take precedence over includes, including members of a selected collection.
- `required: true` groups are immune.

### Staged edits

When an effective configuration is reloaded during a staged edit, v4 roots and includes are read from in-memory overlays that keep their canonical paths and provenance. The overlay is not written to disk.

## V4 selection decision order

Within a v4 resource group, decisions keep include-tree order: included fragments contribute first and the containing file contributes last. The runtime considers dynamic `all`/tag candidates, then applies the ordered stream. An exclusion suppresses matching selections except required resources, and a later explicit include can re-enable the same resource.

`required: true` protects selections but does not select anything itself, so a required-only group selects zero catalog resources. Explicit `tags: []` disables inherited tags, and `harnesses: []` selects no targets. Both stay distinct from omitted fields.

## Layer order

Effective merge order, from lowest to highest precedence:

| Order | Source | File | Notes |
|---|---|---|---|
| 1 | `Organization` | `/etc/agent-marketplace/config.yaml` | Managed externally (for example by MDM) |
| 2 | `UserPersonal` | `~/.agents/config.yaml` | |
| 3 | `RepoCommitted` | `<repo>/.agents/config.yaml` | |
| 4 | `RepoPersonal` | `<repo>/.agents/config.local.yaml` | Supports registries with per-entry selectors and top-level include/exclude |

Higher layers override lower layers where applicable.

### Layer locks

Each layer's remote Git requirements resolve only against that layer's generated lock:

| Layer | Lock |
|---|---|
| Organization | `/etc/agent-marketplace/lock.yaml` |
| User | `~/.agents/lock.yaml` |
| Repository | `.agents/lock.yaml` |
| Personal-local | `.agents/lock.local.yaml` |

Lock composition does not change selection precedence: exact layer catalogs are composed before the merge and selection rules below. Plain local-directory registries stay live and unlocked.

Lock v2 is the only write format for every writable layer, including Git-only layers. A writable v1 lock is upgraded to v2 on the next reconciliation. A read-only organization v1 lock is still accepted and reported with a `lock.version-deprecated` warning. Lock v2 also records exact Marketplace endpoint, registry, resource, and selection state produced by migration, install/remove, and update. Hosted Marketplace rebind and dynamic-selector snapshots are outside this foundation.

### Target harnesses

Top-level target harnesses follow project-first precedence: `RepoCommitted`, then `UserPersonal`, then `Organization`. A layer whose top-level `harnesses` list is absent or empty does not set the target set, so the next layer is consulted. A registry entry's `harnesses:` list narrows the effective target list for that entry.

## Registry allowlist

`allowed_registries` is a first-authority setting. The first layer in merge order that declares it (organization, then user, then repository) fixes the allowlist for the effective config.

- Within that layer, the earliest declaration in include order wins. Later includes and the containing root cannot relax it.
- Later layers cannot broaden or override it.
- `config.local.yaml` cannot declare it.

Its value controls remote origins:

- Omitted (or `nil`) in every layer: no allowlist is configured and all remote registry origins are permitted.
- Explicitly empty (`allowed_registries: []`): all remote registries are denied.
- A list of regex patterns: every non-local registry URL must match at least one pattern. URLs are matched in `host/path` form, without the scheme, `git@` prefix, trailing slash, or `.git` suffix (for example, `git@example.com:team/repo.git` is matched as `example.com/team/repo`).

`allowed_marketplaces` follows the same first-authority rule for Marketplace endpoint URLs, and `config.local.yaml` cannot declare it either.

## Registry-level selection rules

These rules describe a v2/v3 registry entry. A v4 resource group is adapted into the same runtime entry and applies the ordered stream described in [V4 selection decision order](#v4-selection-decision-order).

For each registry entry, selection works per resource kind:

- Top-level kind selectors and `tags` provide defaults for registry entries that omit those values. Defaults in later files cascade to registries from earlier includes, and the last declaration wins.
- An entry's own selector list or `tags` value, including an explicit empty list, replaces the inherited default instead of merging with it. Once inherited or set, `exclude_all: true` stays true.
- The selected names are the union of:
  - all resources, when `(required || default) && no kind-specific selections && tags == nil`
  - explicit per-kind selections (`skills`, `commands`, `subagents`, `hooks`, `mcp`)
  - resources matching `tags`
- Per-kind `exclude` then removes names from the selection, except when `required: true` (required is immune).
- Per-kind `exclude_all: true` removes all resources of that kind from the entry's selection, except when `required: true`.

## ExcludeAll

`exclude_all: true` is available at two levels:

- Per kind (`skills.exclude_all: true`) excludes all resources of that kind from the registry entry. Other kinds (commands, subagents, and so on) are unaffected.
- On the registry entry (`exclude_all: true`) it is shorthand for setting `exclude_all: true` on every per-kind selector, which excludes everything from that registry.

In a v4 resource group, `exclude: ["*"]` is the registry-level form and `exclude: [skill/*]` (or another `kind/*`) is the per-kind form.

At a higher-precedence layer, `exclude_all` on a registry entry is scoped by registry URL and path subtree. It suppresses resources from the same registry at lower-precedence layers only when the excluding path is the same or a component-wise parent. A root entry covers all subpaths, while sibling paths stay independent, which prevents bleed across registries or sibling paths. A concrete `ref` further limits the shadow to that ref; an omitted ref stays a wildcard for legacy exclusions.

Top-level `skills.exclude_all: true` in a legacy `config.local.yaml` applies to all repo-scoped (non-required, non-machine-scope) resources of that kind.

`required: true` resources are immune to `exclude_all` at any layer.

## Exclude precedence

- Excludes from higher-precedence layers suppress lower-layer non-required resources when the source-scope boundary permits it. Repo and checkout-local excludes do not suppress machine-scoped resources.
- `config.local.yaml` per-kind excludes are the strongest personal opt-out for non-required repo-scoped resources.
- No layer can exclude a `required: true` resource.

## Removal scope

### Machine and repo occurrences

Resolution keeps machine-scoped and repo-scoped occurrences separate because they are written to different roots. If the same kind and name is enabled at both scopes, an unscoped `remove` is ambiguous and does not silently remove one copy.

- For a non-required resource, `remove --scope user` selects the machine-scoped occurrence and `remove --scope repo` selects the committed repo occurrence. The other scope stays enabled.
- If the selected scope has several providers, add `--from`/`--path` (and `--kind` when needed).
- `--scope local` selects a checkout-local occurrence or exclusion.
- An explicit scope never falls back to another scope when that scope has no occurrence; it reports that the resource is not enabled. The exception is a machine-owned occurrence targeted from repo or local scope: it is kept so the command can return the actionable scope-boundary refusal.

### Authority boundaries

An explicit scope does not bypass authority boundaries.

- Repo and local config cannot suppress a machine-scoped resource; use `--scope user`.
- Required resources stay immune to removal and exclusion at every scope.
- When no scope is given and there is exactly one occurrence, removal infers its effective layer (machine scope maps to user config).
- A repo-scoped auto-selected resource that the committed repo config does not explicitly select is instead recorded as a checkout-local exclusion. Use `--scope repo` when you intend to edit committed repo config.

### Removal with `--from`

With `--from`, `--scope` chooses the config root to edit and defaults to `repo`.

- The URL, path, and ref identify the configured registry entry. `--path` narrows a named removal to a configured subdirectory and its discovered descendants.
- Omitting `--path` or `--ref` is unambiguous only when the URL identifies one logical configured location. Distinct paths or refs require the corresponding flag.
- For whole-registry removal, provide `--path` (and `--ref` when needed) when the same URL has several entries.
- Diagnostic `@ref` suffixes are labels only. Pass the URL, path, and ref as separate flags.
- The authored URL/path spelling is recommended, although equivalent normalized URL/path spellings identify the same entry.

### Collection removal

Collection removal uses a separate lookup because expansion drops the collection artifact.

- Without `--scope`, it checks the repo, local, and user roots, in that order. `--scope user|repo|local` selects one root.
- Top-level selections are removed or shadowed in that root.
- Organization-only selections infer user scope when unscoped, while an explicit repo or local scope fails the authority check.
- Registry-scoped selections match URL, path, and the supplied `ref`. Path-only local aliases stay registry-scoped.
- Required collections cannot be removed from any scope. Machine-managed collections cannot be removed from repo or local scope.
- The include tree is validated before a top-level edit.

A member with `path` but no `ref` binds deterministically when that path is unique. If several refs provide it, the resolver warns with each `path@ref` candidate and the chosen location. Path-less members keep repository-wide lookup and candidate-path diagnostics.

## Collection precedence

### Expansion and direct selection

- Collections expand into synthetic registry entries before resolution. The legacy `skills:` member list and the typed `resources:` list are both supported and merged.
- Nested includes form a DAG, so a shared child is valid across sibling branches, while an ancestor repeated on one branch is a cycle.
- Removing a collection member writes a per-kind `exclude` in the active scope. The collection stays installed.
- If a resource is both directly selected and provided by a collection, the direct selection takes precedence for conflict handling and the `Via` annotation.

### Members pinned to a path

- A member that names a `registry:` with a `path:` binds to the configured entry at exactly that path. If the repository is configured but not at that path, the warning names both the requested path and the paths that are configured.
- A pinned path that nobody configured has the same severity as a path-less miss, not a harsher one. The pin is an authoring detail of a collection the consumer usually cannot edit, so it must not be able to fail their sync on its own. It is a soft warning unless any configured location for that repository is `required: true`, exactly as for a path-less miss, so pinning a path can never bypass `required: true` policy.

### Members pinned to a ref

A member that pins a `ref:` binds only to entries at that ref, or to entries that leave `ref` unset (as local registries may). It never silently falls back to a different ref's content. If no configured entry uses the ref, the miss is reported and names the ref.

### Eligible binding targets

- A location the user scoped away with `exclude_all` (what `agent-marketplace remove --from <url> [--path <p>]` writes) is not an eligible binding target, so a collection cannot reintroduce a resource from a directory the user removed. `required: true` entries stay immune, here as everywhere else.
- When several eligible entries provide the member, a `required: true` entry wins over one that only comes first in config order, so binding never strips the required status from an organization-mandated artifact.
- A member miss is suppressed when that exact `(kind, name)` is resolved anyway, whether directly or through another collection or registry. This mirrors the cross-registry suppression applied to collection lookups themselves.

### Members that name a registry without a path

A member that names a `registry:` without a `path:` means "this repository, wherever the resource is". It binds to whichever configured entry for that repository contains the member, independent of config order and of a `.git` suffix difference. This matters for monorepos, where recursive discovery and organization team profiles can both contribute many entries at different paths for one URL.

- If no configured entry contains the member, the warning names every path searched instead of blaming one, and does not claim an upstream removal.
- Such an unbindable member is a hard (sync-failing) error as soon as any configured location for that repository is `required: true`, and a soft warning otherwise. Required status is judged per location, not per config entry, so two spellings of one location (with and without `.git`) count as one location.
- If more than one distinct location contains the member, the ambiguity is reported as a warning. The winner is chosen by `required: true`, then config source/file precedence, then config order. Pin a `path:` on the member to make the choice explicit.

### Nested collection definitions

Nested collection definitions use configured registry ownership, not catalog or cache-key order. For duplicates, `required: true` wins, followed by config source/file precedence.

- If authority ties, the resolver prefers an owner matching the parent URL/ref/path, then uses stable URL/path order.
- Recursively expanded members inherit the selected URL, path, ref, and required policy.
- Harness targets are inherited when the nested owner leaves them unset. An explicit list on the nested owner can only narrow the containing collection's targets and is intersected with the effective target set. An explicit empty list stays an opt-out.
- Before recursion, nested collection definitions obey the same exclusions as direct members: collection-name excludes, top-level or local `collections.exclude_all`, and higher-authority per-registry or registry-level `exclude_all` suppress the definition and all of its members. A `required: true` nested owner stays immune.
- Include ancestry is branch-local. A collection shared by sibling include branches is valid and may expand under each branch, while an ancestor repeated along one branch is reported as a cycle. Members are still deduplicated by the normal direct/collection resolution rules.

## Resource prerequisites

Catalog resources may declare prerequisites in their frontmatter metadata:

```yaml
metadata:
  requires:
    - kind: mcp
      name: genai-atlassian
      registry: git@example.com:agents/registry.git
      path: integrations
```

The shorthand `"mcp:genai-atlassian"` is equivalent when no provider hint is needed. `kind` defaults to `skill`.

- `install`, `sync`, `status`, and `doctor` report selected resources whose prerequisites are not selected.
- Prerequisite metadata is advisory. No command enables a prerequisite, or removes an explicit exclude, because another resource requires it.
- Only configuration and direct install/remove operations control explicit selection and exclusion.
- Satisfaction is determined by resource kind and name. `registry` and `path` are remediation hints used in the suggested install command.
- `metadata.requires` is distinct from registry `required: true`. The first expresses a functional dependency; the second enforces selection.

## Name collisions and ambiguity

### Non-hook resources

If the same resource name (within the same kind) is selected from several different registries, resolution emits a soft warning, chooses one deterministic winner, and continues writing that resource. The winner is chosen by:

1. `required: true`
2. higher config source/file precedence
3. the alphabetically first normalized registry URL
4. stable config order, if the criteria above tie

Collision warnings name the winner and every distinct registry/path participant, including paths that were otherwise deduplicated for output. If the same normalized URL and path are used at several refs, every non-empty ref label also includes `@<ref>` so the participants stay distinguishable.

If same-registry entries at different paths share a scope, all occurrences are kept for visibility, but only one can be written in that scope; a warning identifies the chosen path and the other paths. Machine-scoped and repo-scoped occurrences may both remain because they are written to different directories.

### Hooks

Hooks are additive across independent registry sources, including distinct non-overlapping paths, even when their generated names, events, types, or matchers match.

- Names stay registry-local selectors.
- Repeated references to the same logical source still deduplicate. Competing refs at the same normalized URL/path within one installation scope keep precedence and collision warnings.
- Direct selections and collection members use the same hook reconciliation policy.
- Use registry-scoped includes/excludes or `remove --from` to target one provider.
- `install --all --kind hook` selects every applicable provider. Ordinary named installation keeps its existing provider-selection behavior.
- This is a resolution contract. Target-specific compilation diagnostics and ownership rules still apply.
- Windsurf's legacy selector stays bound to its previous last-handler survivor, and recovered siblings receive numeric suffixes.
- Existing source-revision v1/v2 locks keep the old inventory until an explicit `lock --upgrade` or `update`. New v3 inventories preserve the additional handlers.

### Overlapping paths

Entries with the same normalized URL and ref whose paths overlap by whole path components (for example, `teams/dev` and `teams/dev/eng`) are treated as one physical source, not as a name collision. Required policy wins first. Among entries with equal required status, the more specific path wins when both provide a name. Overlapping entries in different scopes are complementary and stay separate so both scopes can be written.

### Remediation

Collision remediation drops the display-only path/ref suffix from the source label and uses `agent-marketplace remove <name> --from <url>`, adding `--path <path>` and/or `--ref <ref>` when needed. The `@<ref>` suffix is a diagnostic label and is not part of the URL passed to `--from`. Pass the URL, path, and ref as separate flags, using the config's authored URL/path spelling when targeting an entry.

If the same name exists in different kinds (for example, a skill named `deploy` and a command named `deploy`), use `--kind skill|command|subagent|hook|mcp` to disambiguate.

## Practical examples

### Org required + repo exclude

`required: true` wins. The resource stays enabled.

### User default + repo exclude

The repo exclude suppresses the user-default resource, which is not required.

### User default + local exclude

The local exclude suppresses it for that checkout only.
