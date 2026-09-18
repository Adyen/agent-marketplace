# Selection & Precedence Reference

This document describes how `agent-marketplace` decides which resources are enabled, where they are sourced from, and which excludes/removals win. All five target-materialized kinds (skills, commands, subagents, hooks, and MCP servers) follow the same rules; collections are catalog-selectable bundles, not materialized resources.

## Schema requirement

Legacy configuration uses the **v2 schema** (`apiVersion: agent-marketplace/v2`); `agent-marketplace/v3` remains accepted as a compatibility alias. Generated legacy `config.local.yaml` roots may omit `apiVersion`; if present, it must be v2 or v3. Legacy included fragments may also omit `apiVersion`, but regular legacy roots without it are rejected.

V4 is a separate, explicit source/resource schema. A v4 root must declare `apiVersion: agent-marketplace/v4`; v4 included fragments may omit the version, but an explicit v2/v3 version or legacy installation key is rejected. Normal CLI readers recognize v4 and adapt each layer into the runtime requirement model. V4 sources and resource groups are composed within their root/include tree before the normal layer order is applied; v4 does not make legacy `registries`, `skills`, or `marketplaces` keys valid.

`agent-marketplace migrate` targets v4 by default; `--to v4` remains an accepted compatibility spelling. Proactive CLI and TUI migration is delayed until resource editors support v4, so startup leaves writable v2/v3 roots (and unversioned local legacy roots) unchanged and continues using the legacy reader. Use `agent-marketplace migrate --to v2` only for the legacy structural-remediation flow.

## Config includes

All four config layers accept `include` as one local path or an ordered list of local paths:

```yaml
apiVersion: agent-marketplace/v2
include: profiles/company.yaml
```

Use a sequence when order matters:

```yaml
include:
  - profiles/company.yaml
  - profiles/payments-team.yaml
```

Relative paths resolve from the directory containing the file that declares them; absolute paths are accepted. Includes are recursive. Legacy include trees are identified by canonical path, so the same file reached more than once, including through a symlink or normalized relative path, contributes only at its first position. V4 include trees retain branch-local ancestry instead: shared fragments expand once per include branch, and only an ancestor repeated on the current branch is a cycle. Glob patterns and remote URLs are not supported.

Organization includes have an additional trust requirement, whether or not Git registries or an organization lock are present. External relative and absolute paths remain supported, but every included file must be a root-owned regular file with no group or other write permission. Every lexical ancestor directory through the filesystem root must also be root-owned, non-symlink, and not group- or other-writable. Validation precedes symlink resolution and deduplication; a symlink or writable directory before `..` is not bypassed by path normalization. Unsafe fragments fail the organization layer with an `organization.trust.*` diagnostic. System symlink aliases such as macOS `/etc`, `/var`, and `/tmp` are not exceptions for included paths; use trusted physical paths such as `/private/etc` instead. User, repository, and personal-local include behavior is unchanged.

Each included fragment has lower precedence than the file containing it. For a list, the first fragment is merged first, later fragments merge over earlier ones, and the containing file merges last. Recursive includes follow the same rule. Merging continues to use field-specific rules; `include` does not turn every field into a simple replacement. For top-level collection selectors and local resource selectors, later files decide conflicts for the same name. Within one file, `exclude` wins over `include`. Once any contributing file sets `exclude_all: true`, it remains true.

Loading the owning layer fails if any include is missing, unreadable, malformed, invalid for that layer, or part of a direct or indirect cycle. Included fragments may omit `apiVersion` as described above; this exception does not relax validation for a regular root.

The CLI reads included content when calculating effective configuration, but config-editing commands modify only the selected root. They do not rewrite included fragments. Removing an include-owned named resource or collection records an exclusion in the selected root; whole-registry `remove --from` writes a registry-level `exclude_all` shadow there. The included file remains unchanged, and normal scope boundaries still apply.

When `url` is omitted, a registry entry uses `path` as its location alias (for example, `- path: /opt/agent-skills`). With `url` present, `path` names a subdirectory. Config editors and removals preserve this alias-versus-subdirectory interpretation; use the authored URL/path spelling when targeting an entry. Resolver matching is separate: equivalent registry-path spellings such as `./foo/`, `foo/.`, repeated or mixed separators, and the root aliases `.`/empty are canonicalized to the same path identity; this does not rewrite the authored config.

## Explicit v4 migration safety

`agent-marketplace migrate` (or `agent-marketplace migrate --to v4`) is a format-only conversion, not a selection upgrade. Without `--scope`, it validates the writable user, repository, and repository-personal roots as one transaction; `--scope user|repo|local` limits validation and publication to that layer. Organization configuration is never read or written by this conversion. Git/directory-only conversion leaves generated locks unchanged; exact Marketplace pin migration publishes each migrated config and its owning layer lock (created or upgraded to `agent-marketplace-lock/v2`) together in one journaled dependency transaction. `--dry-run` performs the same representability and v4-reader checks and writes neither config nor lock. Until resource editors support v4, new user configs created by `agent-marketplace init` use the editable v2 schema.

Conversion is fail-closed. The migrator refuses a legacy include tree because flattening it or editing its fragments would change provenance, refuses legacy hosted Marketplace selections when their pin provenance cannot be proven from verified local cache evidence, refuses collection-derived aliases that cannot be represented without creating new direct intent, and refuses any source, selector, revision, or policy that cannot be represented without semantic loss. A refusal or validation failure publishes none of the selected files; the legacy configuration remains usable unchanged.

V4 roots are read-compatible with normal CLI discovery, status, and synchronization for Git and directory sources. Source kinds are explicit: use `git`, `directory`, or `marketplace`; a live directory is not inferred from a `.git` suffix. Directory sources resolve relative to the declaring config file, remain live and unlocked, and do not accept Git-only `ref` or `path` fields. Marketplace sources are runtime-resolvable only for explicit requests backed by a matching normalized `agent-marketplace-lock/v2` record in the owning layer's lock; a missing lock, or any endpoint, scope, binding, revision, or selection mismatch, fails closed (`v4.marketplace-lock-required` / `v4.marketplace-lock-mismatch`). Dynamic selectors and v4 Marketplace editing remain unsupported. Legacy configuration writers return `v4.writer-opt-in` instead of rewriting v4 roots.

When an effective configuration is reloaded during a staged edit, v4 roots and includes are read from supplied in-memory overlays while retaining their canonical paths and provenance. The overlay is not written to disk.

## V4 selection decision order

Within a v4 resource group, decisions retain include-tree order: included fragments contribute first and the containing file contributes last. The runtime considers dynamic `all`/tag candidates, then applies the ordered stream. An exclusion suppresses matching selections except required resources; a later explicit include can re-enable the same resource.

`required: true` protects selections but is not itself a selection expression, so a required-only group selects zero catalog resources. Explicit `tags: []` disables inherited tags, and `harnesses: []` selects no targets; both remain distinct from omitted fields.

## Layer order

Effective merge order (low → high):

1. `OrgMandatory` (`/etc/agent-marketplace/config.yaml`, MDM-managed)
2. `UserPersonal` (`~/.agents/config.yaml`)
3. `RepoCommitted` (`<repo>/.agents/config.yaml`)
4. `RepoPersonal` (`<repo>/.agents/config.local.yaml`, supports registries with per-entry selectors and top-level include/exclude)

Higher layers override lower layers where applicable.

Each layer's remote Git requirements are resolved only against that layer's generated lock: organization `/etc/agent-marketplace/lock.yaml`, user `~/.agents/lock.yaml`, repository `.agents/lock.yaml`, and personal-local `.agents/lock.local.yaml`. Lock composition does not change selection precedence: exact layer catalogs are composed before the normal merge and selection rules below. Plain local-directory registries remain live and unlocked. Lock v2 additionally records exact Marketplace endpoint/registry/resource/selection state produced by explicit v4 migration and backs the read-only v4 Marketplace runtime adapter; hosted Marketplace lock acquisition, update, rebind, and dynamic-selector integration remain outside this foundation.

Top-level target harnesses follow project-first precedence: `RepoCommitted`
then `UserPersonal`, then `OrgMandatory`. A registry entry's `harnesses:`
list narrows the effective target list for that entry.

## Registry allowlist

`allowed_registries` is a first-authority setting. The first layer in merge order that declares it (org, then user, then repo) fixes the allowlist for the effective config. Within that layer, the earliest declaration in include order wins; later includes and the containing root cannot relax it. Later layers cannot broaden or override it, and `config.local.yaml` cannot declare it.

- When `allowed_registries` is omitted (or `nil`) in every layer, no allowlist is configured and all remote registry origins are permitted.
- When `allowed_registries` is explicitly set to an empty list (`allowed_registries: []`), all remote registries are denied.
- When `allowed_registries` contains regex patterns, every non-local registry URL must match at least one pattern to be allowed.

## Registry-level selection rules

For each registry entry, selection works per resource kind:

- top-level kind selectors and `tags` provide defaults for registry entries that omit those values. Defaults in later files cascade to registries from earlier includes; the last declaration wins
- an entry's own selector list or `tags` value, including an explicit empty list, replaces the inherited default rather than merging with it. Once inherited or set, `exclude_all: true` remains true
- selected names = union of:
  - auto-select all when `(required || default) && no kind-specific selections && tags == nil`
  - explicit per-kind selections (`skills`, `commands`, `subagents`, `hooks`, `mcp`)
  - resources matching `tags`
- then per-kind `exclude` removes from selected names, except when `required: true` (required is immune)
- per-kind `exclude_all: true` removes ALL resources of that kind from the entry's selection (except when `required: true`)

## ExcludeAll

`exclude_all: true` is available at two levels:

- **Per-kind** (`skills.exclude_all: true`): excludes all resources of that kind from the registry entry. Other kinds (commands, subagents, etc.) are unaffected.
- **Registry-level** (`exclude_all: true` on the entry): sugar that sets `exclude_all: true` on all per-kind selectors, excluding everything from that registry.

At a higher-precedence layer, `exclude_all` on a registry entry is **scoped by registry URL and path subtree**: it suppresses resources from the same registry at lower-precedence layers only when the excluding path is the same or a component-wise parent (a root entry covers all subpaths, while sibling paths remain independent), preventing cross-registry or sibling-path bleed. A concrete `ref` further limits the shadow to that ref; an omitted ref remains a wildcard for legacy exclusions. Top-level `skills.exclude_all: true` in `config.local.yaml` applies to all repo-scoped (non-required, non-machine-scope) resources of that kind.

`required: true` resources are immune to `exclude_all` at any layer.

## Exclude precedence

- Excludes from higher-precedence layers suppress lower-layer non-required
  resources when the source-scope boundary permits it; repo and checkout-local
  excludes do not suppress machine-scoped resources.
- `config.local.yaml` per-kind excludes are the strongest personal opt-out for
  non-required repo-scoped resources.
- `required: true` resources cannot be excluded by any layer.

## Removal scope

- Resolution keeps machine- and repo-scoped occurrences separate because they
  materialize to different roots. If the same kind and name is enabled at both
  scopes, an unscoped `remove` is ambiguous rather than silently removing one
  copy. For a non-required resource, `remove --scope user` selects the
  machine-scoped occurrence and `remove --scope repo` selects the committed
  repo occurrence; the other scope remains enabled. If the selected scope has
  multiple providers, add `--from`/`--path` (and `--kind` when needed).
  `--scope local` selects a checkout-local occurrence or exclusion. An
  explicit scope never falls back to another scope when that scope has no
  occurrence; it reports that the resource is not enabled, except that a
  machine-owned occurrence is retained to return the actionable scope-boundary
  refusal when targeted from repo or local scope.
- Explicit scope does not bypass authority boundaries. Repo and local config
  cannot suppress a machine-scoped resource; use `--scope user`. Required
  resources remain immune to removal and exclusion at every scope. When no
  scope is given and there is only one unambiguous occurrence, removal infers
  its effective layer (machine scope maps to user config); a repo-scoped
  auto-selected resource that is not explicitly selected by the committed repo
  config is instead recorded as a checkout-local exclusion. Use `--scope repo`
  when you intend to edit committed repo config.
- With `--from`, `--scope` chooses the config root to edit and defaults to
  `repo`. The URL, path, and ref identify the configured registry entry;
  `--path` narrows a named removal to a configured subdirectory and its
  discovered descendants. Omitting `--path` or `--ref` is only unambiguous
  when the URL identifies one logical configured location; distinct paths or
  refs require the corresponding flag. For whole-registry removal, provide
  `--path` (and `--ref` when needed) when the same URL has multiple entries.
  Diagnostic `@ref` suffixes are labels only; pass the URL, path, and ref as
  separate flags. The authored URL/path spelling is recommended, although
  equivalent normalized URL/path spellings identify the same entry.

Collection removal uses a separate lookup because expansion drops the
collection artifact. Without `--scope`, it checks repo, local, then user roots;
`--scope user|repo|local` selects a root. Top-level selections are removed or
shadowed there. Organization-only selections infer user scope when unscoped,
while explicit repo/local scope fails the authority check. Registry-scoped
selections match URL, path, and supplied `ref`; path-only local aliases stay
registry-scoped. Required collections and machine-managed collections cannot be
removed from any scope or from repo/local scope, respectively. The include tree
is validated before a top-level edit.

A member with `path` but no `ref` binds deterministically when that path is
unique. If multiple refs provide it, the resolver warns with each `path@ref`
candidate and the chosen location. Path-less members retain repository-wide
lookup and candidate-path diagnostics.

## Collection precedence

- Collections expand into synthetic registry entries before resolution. The
  legacy `skills:` member list and the typed `resources:` list are both
  supported and merged; nested includes form a DAG, so a shared child is valid
  across sibling branches while an ancestor repeated on one branch is a cycle.
- Removing a collection member writes a per-kind `exclude` in the active scope (collection stays installed).
- If a resource is both direct-selected and collection-provided, direct selection takes precedence for conflict handling and `Via` annotation.
- A member that names a `registry:` with a `path:` binds to the configured entry at exactly that path. If the repository is configured but not at that path, the warning names both the path the collection asked for and the paths that *are* configured.
  - A pinned path nobody configured carries the same severity as a path-less miss, not a harder one: the pin is an authoring detail of a collection the consumer usually cannot edit, so it must not be able to fail their sync on its own. It is a soft warning unless **any** configured location for that repository is `required: true`, exactly as for a path-less miss, so `required: true` policy can never be bypassed by pinning a path.
- A member that pins a `ref:` only binds to entries at that ref (or to entries that leave `ref` unset, as local registries may). It never falls back to a different ref's content silently; if no configured entry uses the ref, the miss is reported and names the ref.
- A location the user scoped away with `exclude_all` (what `agent-marketplace remove --from <url> [--path <p>]` writes) is not an eligible binding target, so a collection cannot re-introduce a resource from a directory the user removed. `required: true` entries stay immune, here as everywhere else.
- When several eligible entries provide the member, a `required: true` entry wins over one that merely comes first in config order, so binding never strips an org-mandated artifact of its required-ness.
- A member miss is suppressed when that exact `(kind, name)` is resolved anyway — directly, or through another collection or registry — mirroring the cross-registry suppression applied to collection lookups themselves.
- A member that names a `registry:` **without** a `path:` means "this repository, wherever the resource actually is": it binds to whichever configured entry for that repository actually contains the member, independent of config order and of a `.git` suffix difference. This matters for monorepos, where recursive discovery and org team profiles can both contribute many entries at different paths for one URL.
  - If no configured entry contains it, the warning names every path searched instead of blaming one, and does not claim an upstream removal.
  - Such an unbindable member is a hard (sync-failing) error as soon as **any** configured *location* for that repository is `required: true`, and a soft warning otherwise. Required-ness is judged per location, not per config entry, so two spellings of one location (with and without `.git`) are one location.
  - If more than one distinct location contains it, the ambiguity is reported as a warning and the winner is chosen by `required: true`, then config source/file precedence, then config order. Pin a `path:` on the member to make it explicit.
- Nested collection definitions use configured registry ownership, not catalog or cache-key order. For duplicates, `required: true` wins, followed by config source/file precedence.
  - If authority ties, the resolver prefers an owner matching the parent URL/ref/path, then uses stable URL/path order.
  - Recursively expanded members inherit the selected URL, path, ref, and required policy. Harness targets are inherited when the nested owner leaves them unset; an explicit nested-owner list can only narrow the containing collection's targets, and is intersected with the effective target set. An explicit empty list remains an opt-out.
  - Before recursion, nested collection definitions obey the same exclusions as direct members: collection-name excludes, top-level/local `collections.exclude_all`, and higher-authority per-registry or registry-level `exclude_all` suppress the definition and all of its members. A `required: true` nested owner remains immune.
  - Include ancestry is branch-local: a collection shared by sibling include branches is valid and may be expanded under each branch, while an ancestor repeated along one branch is reported as a cycle. Members are still deduplicated by the normal direct/collection resolution rules.

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

The shorthand `"mcp:genai-atlassian"` is equivalent when no provider hint is
needed. `kind` defaults to `skill`.

- `install`, `sync`, `status`, and `doctor` report selected resources whose
  prerequisites are not selected.
- Prerequisite metadata is advisory. No command enables a prerequisite or
  removes an explicit exclude because another resource requires it.
- Explicit selection and exclusion remain controlled only by configuration and
  direct install/remove operations.
- Satisfaction is determined by resource kind and name. `registry` and `path`
  are remediation hints used in the suggested install command.
- `metadata.requires` is distinct from registry `required: true`: the former
  expresses a functional dependency, while the latter enforces selection.

## Name collisions and ambiguity

- For resource kinds other than hooks, if the same resource name (within the same kind) is selected from multiple different registries, resolution emits a soft warning, chooses one deterministic winner, and continues materializing the resource. The winner is selected by `required: true`, then higher config source/file precedence, then alphabetically first normalized registry URL; if those criteria tie, stable config order wins.
- Collision warnings name the winner and every distinct registry/path participant, including paths that were otherwise deduplicated for materialization. If the same normalized URL and path are used at multiple refs, every non-empty ref label also includes `@<ref>` so the participants remain distinguishable. For non-hook resources, if same-registry entries at different paths share a scope, all occurrences are retained for visibility but only one can materialize in that scope; a warning identifies the chosen path and the other paths. Machine- and repo-scoped occurrences may both remain because they materialize to different directories.
- Hooks are additive across independent registry sources, including distinct non-overlapping paths, even when their generated names, events, types, or matchers match. Names remain registry-local selectors. Repeated references to the same logical source still deduplicate; competing refs at the same normalized URL/path within one installation scope retain precedence and collision warnings. Direct selections and collection members use the same hook reconciliation policy. Use registry-scoped includes/excludes or `remove --from` to target one provider. `install --all --kind hook` selects every applicable provider; ordinary named installation retains its existing provider-selection behavior. This is a resolution contract: target-specific compilation diagnostics and ownership rules still apply. Windsurf's legacy selector remains bound to its previous last-handler survivor; recovered siblings receive numeric suffixes. Existing source-revision v1/v2 locks retain the old inventory until an explicit `lock --upgrade` or `update`; new v3 inventories preserve the additional handlers.
- Same normalized URL+ref entries whose paths overlap by whole path components (for example, `teams/dev` and `teams/dev/eng`) are treated as one physical source rather than a name collision. Required policy wins first; among entries with equal required status, the more-specific path wins when both provide a name. Overlapping entries in different scopes are complementary and remain separate so both scopes can materialize.
- Collision remediation removes the display-only path/ref suffix from the source label. It uses `agent-marketplace remove <name> --from <url>` and adds `--path <path>` and/or `--ref <ref>` when needed. The `@<ref>` suffix is diagnostic labeling, not part of the URL passed to `--from`; pass the URL, path, and ref as separate flags, using the config's authored URL/path spelling when targeting an entry. If the same name exists across different kinds (e.g. a skill named `deploy` and a command named `deploy`), use `--kind skill|command|subagent|hook|mcp` to disambiguate.

## Practical examples

### Org required + repo exclude

`required: true` wins. Resource remains enabled.

### User default + repo exclude

Repo exclude suppresses the user-default resource (non-required).

### User default + local exclude

Local exclude suppresses it for that checkout only.
