# Selection & Precedence Reference

This document describes how `agent-marketplace` decides which resources are enabled, where they are sourced from, and which excludes/removals win. All five target-materialized kinds (skills, commands, subagents, hooks, and MCP servers) follow the same rules; collections are catalog-selectable bundles, not materialized resources.

## Schema requirement

The **v2 schema** (`apiVersion: agent-marketplace/v2`) is the canonical format for organization, user, and committed repository configs. `agent-marketplace/v3` is still accepted as a compatibility alias. Generated `config.local.yaml` files use the local-config schema and may omit `apiVersion`; if present, it must be v2 or v3. The loader rejects v1 configs (no `apiVersion`) in the regular config layers. `agent-marketplace migrate` performs structural remediation (for example, removing layer-forbidden top-level fields), not a schema bump, and leaves valid v2 files untouched.

## Layer order

Effective merge order (low → high):

1. `OrgMandatory` (`/etc/agent-marketplace/config.yaml`, MDM-managed)
2. `UserPersonal` (`~/.agents/config.yaml`)
3. `RepoCommitted` (`<repo>/.agents/config.yaml`)
4. `RepoPersonal` (`<repo>/.agents/config.local.yaml`, supports registries with per-entry selectors and top-level include/exclude)

Higher layers override lower layers where applicable.

Top-level target harnesses follow project-first precedence: `RepoCommitted`
then `UserPersonal`, then `OrgMandatory`. A registry entry's `harnesses:`
list narrows the effective target list for that entry.

## Registry allowlist

`allowed_registries` is a first-authority setting: the first layer in precedence order that declares it (org, then user, then repo) fixes the allowlist for the whole effective config. Lower layers cannot broaden or override it, and `config.local.yaml` is not allowed to declare `allowed_registries`.

- When `allowed_registries` is omitted (or `nil`) in every layer, no allowlist is configured and all remote registry origins are permitted.
- When `allowed_registries` is explicitly set to an empty list (`allowed_registries: []`), all remote registries are denied.
- When `allowed_registries` contains regex patterns, every non-local registry URL must match at least one pattern to be allowed.

## Registry-level selection rules

For each registry entry, selection works per resource kind:

- top-level `skills/commands/subagents/hooks/mcp/tags/exclude` in that config are defaults for entries that do not set their own values
- if entry sets its own kind-specific list (e.g. `skills`), entry values replace (not merge with) top-level defaults for that entry and kind
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

At a higher-precedence layer, `exclude_all` on a registry entry is **scoped by registry URL**: it only suppresses resources from the same registry at lower-precedence layers, preventing cross-registry bleed. Top-level `skills.exclude_all: true` in `config.local.yaml` applies to all repo-scoped (non-required, non-machine-scope) resources of that kind.

`required: true` resources are immune to `exclude_all` at any layer.

## Exclude precedence

- Excludes from higher-precedence layers suppress lower-layer non-required resources.
- `config.local.yaml` per-kind excludes are the strongest personal opt-out for non-required resources.
- `required: true` resources cannot be excluded by any layer.

## Collection precedence

- Collections expand into synthetic registry entries before resolution.
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
    - This rule used to require **every** location to be `required: true`. That was bypassable: `required: true` exists to enforce org-wide security standards, and adding a non-required entry for the same repository at a different path in `.agents/config.local.yaml` — gitignored and personal — downgraded an org-mandated hard failure to a warning. A severity a personal config layer can soften is not enforcement, so the quantifier is `any`.
    - The accepted cost is escalation: one `required: true` entry anywhere in a monorepo makes an *unbindable* member of that repository a hard failure, even when the mandated subdirectory is unrelated to the member. It is bounded to members that bind nowhere — a member that binds to some location is unaffected, and so is every member of a repository with no required entry — and the remedy is visible in the message: pin a `path:` on the member, or configure a registry entry for the subdirectory that provides it.
  - If more than one distinct location contains it, the ambiguity is reported as a warning and the first location — preferring a `required: true` one — is used. Pin a `path:` on the member to make it explicit.

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

- If the same resource name (within the same kind) is selected from multiple different registries, resolution errors.
- The CLI does not auto-pick a winner across different registries.
- Resolve ambiguity by making selection registry-specific (e.g. per-entry `skills`), or by excluding one source.
- The same name may exist across different kinds (e.g. a skill named `deploy` and a command named `deploy`). Use `--kind skill|command|subagent|hook|mcp` to disambiguate.

## Practical examples

### Org required + repo exclude

`required: true` wins. Resource remains enabled.

### User default + repo exclude

Repo exclude suppresses the user-default resource (non-required).

### User default + local exclude

Local exclude suppresses it for that checkout only.
