# Sources and resources

Every agent-marketplace v4 configuration is built from two maps that answer two different questions. The `sources` map says where content comes from and how it is acquired. The `resources` map says what to take from each source and with what protections. A source selects nothing on its own, and a resource group cannot name content without a source to load it from.

This page explains what each concept is, what each does during a `sync`, and how they differ; the [repository configuration reference](../reference/configuration.md) documents every field.

## Two maps, two jobs

```yaml
apiVersion: agent-marketplace/v4
harnesses: [claude]
sources:                    # WHERE: named acquisition declarations
  skills:
    git: https://github.com/anthropics/skills.git
    ref: main
    path: skills
resources:                  # WHAT: selection policy, bound by name
  skills:
    include:
      - skill/frontend-design
```

The two maps are joined by name. The group `resources.skills` binds to the source `sources.skills`, and its selections are resolved against that source only.

| | `sources` | `resources` |
|---|---|---|
| Question answered | Where does content come from? | What do we take from it? |
| Each entry is called | A source | A resource group |
| Keyed by | A name you choose | A source name that must exist |
| Owns | Acquisition: URL, ref, path, source layout | Selection: includes, excludes, tags, protections |
| Recorded in the lock | Yes, for Git and Marketplace sources | No; selections are re-derived from config every run |
| Selects anything by itself | No | Yes: `include`, `tags`, and `all` select |
| Can exist without the other | Yes (see below) | No: `v4.undefined-source` is an error |

## What a source is

A source is a named declaration under `sources` that says how to reach one body of content. The name must match `[A-Za-z0-9][A-Za-z0-9._-]*` (start with a letter or digit). Each declaration contains exactly one acquisition kind, `git`, `directory`, or `marketplace`; mixing kinds, or adding a field that belongs to another kind, fails with `v4.source-discriminator` or `v4.source-field`.

Source names are local configuration references. The installation scope comes from which configuration layer declares the source, and the durable identity comes from what the source points at. Renaming a source changes only the label; the lock, the cache, and the generated files are unaffected because none of them are keyed by name.

### `git`

```yaml
sources:
  skills:
    git: https://github.com/anthropics/skills.git
    ref: main
    path: skills
    source_harness: claude
```

A Git source is a remote repository at one URL:

- `ref` (required) names a branch, tag, or commit. The first `sync` resolves it to one exact commit.
- That commit is recorded in the layer's lockfile together with a digest of every resource found at the source's roots. Later syncs keep the locked commit even when `ref` has moved; only `update` (or a lock refresh) advances it.
- `path` narrows discovery to one subdirectory of the repository. The same repository at different paths can be declared as different sources.
- `source_harness` fixes the layout when the repository's native layout is ambiguous. Omit it to let the CLI autodetect.

Two source names may hold compatible declarations for the same repository. They remain separate configuration bindings, but compatible Git acquisition is shared: declaring the same URL at two refs produces two lock records, while declaring the same URL and ref twice produces one.

### `directory`

```yaml
sources:
  local:
    directory: ./agent-resources
```

A directory source is a live local folder:

- Content is read from disk on every command. There is no ref, no commit, and no lock record; whatever is in the folder now is what a `sync` sees.
- In repository `.agents/config.yaml` and `.agents/config.local.yaml` roots, the path resolves relative to the repository root (`directory: .` is the repository itself). In user and organization configs and in included fragments, it resolves relative to the directory of the declaring file.
- `source_harness` is available when the layout is ambiguous, as for Git sources.

Because a directory source is live, it never goes stale: there is no drift to check and nothing to pin. The trade-off is reproducibility: two machines see the same directory source only if their folders hold the same content.

### `marketplace`

```yaml
sources:
  company:
    marketplace: https://marketplace.example.com
resources:
  company:
    include:
      - shared-agents/skill/review:
          revision: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

A Marketplace source is a hosted endpoint that curates and serves resources:

- Every installed resource is pinned to an exact content revision (`sha256:…`) recorded in the layer lock. Resolution fails closed when lock evidence is missing or does not match the requested endpoint, registry, resource, and revision.
- Marketplace resources use three-part locators, `<registry-slug>/<kind>/<name>`, because one endpoint serves many registries.
- A Marketplace source can also be declared exclusion-only: a group with `exclude` and nothing else. Such a source needs no lock, never gets a lock record, and suppresses optional selections from the same endpoint. This override is valid only in user configuration.

See the [hosted Marketplaces explanation](./git-registries-and-hosted-marketplaces.md) for the trust model and the [configuration reference](../reference/configuration.md) for the full Marketplace rules, including `registries` bounds on dynamic selection.

### Declared but never selected

A source does not need a resource group. Acquisition and selection are independent, so what happens depends on the kind:

- A **Git** source with no group is still resolved, acquired, and locked. The commit is recorded and verified on every sync; nothing is selected from it, so nothing materializes.
- A **Directory** source with no group is still walked on every run. If the folder holds a recognizable layout it is simply not selected from; if it holds nothing recognizable the run fails with an ingestion error, because the source is live and must be readable.
- A **Marketplace** source with no group behaves like an acquiring source (it needs lock evidence); use the exclusion-only form when you only want to suppress selections.

If you want no acquisition at all, remove the source declaration. If you want acquisition without selection, a source with no group is fine and keeps the content pinned.

## What a resource is

The word *resource* appears in two roles, and the configuration schema uses it for the second:

1. **A resource** is one installable thing, of one of six kinds. This is what the registry ships and what lands in your harness.
2. **A resource group** is an entry under the `resources` map: the selection policy bound to one source name.

### The six resource kinds

| Kind | What it is | What a sync writes for it |
|---|---|---|
| `skill` | A directory with a `SKILL.md` (name, description, optional metadata) | `skills/<name>/` in each target harness |
| `command` | A prompt command in the source harness's native format | The target's command path, e.g. `.claude/commands/<name>.md` |
| `subagent` | A scoped agent definition | The target's agent path, e.g. `.claude/agents/<name>.md` |
| `hook` | An event hook or hook group | Entries in the target's hook aggregate, e.g. `.claude/hooks/hooks.json` |
| `mcp` | An MCP server definition | Entries in the target's MCP aggregate, e.g. `.mcp.json` |
| `collection` | A named bundle of other resources (`collections/<name>/COLLECTION.yaml`) | Nothing; it expands into its members before selection |

Kinds are independent namespaces: a skill and a command may share one name. A collection is the only kind that never materializes itself; including `collection/onboarding` is shorthand for including its members. The [registry layout reference](../reference/registry-layout.md) lists where each kind is discovered, and [harnesses and output paths](../reference/harnesses-and-output-paths.md) lists where each kind lands per target.

### Resource groups

Every group is keyed by a source name in the same root and include tree, and holds the selection policy for that one source:

```yaml
resources:
  skills:
    include:
      - skill/frontend-design
    tags: [design]
    exclude:
      - skill/legacy-design
    harnesses: [claude, cursor]
    required: true
```

| Field | What it does |
|---|---|
| `include` | Explicit requests, as locator strings or with options. A bare list of locators is equivalent to `include`. |
| `exclude` | Removes matching non-required selections. `*` excludes everything, `kind/*` excludes one kind. |
| `tags` | Selects resources whose `metadata.tags` contain a matching tag. |
| `all` | Selects every resource in the source, every kind. |
| `registries` | Bounds dynamic (`all`/`tags`) Marketplace selection to named registry slugs. Selects nothing alone. Invalid for Git and directory sources. |
| `required` | Protects this group's selections from excludes and removal. Selects nothing by itself; a required-only group selects zero resources. |
| `harnesses` | Narrows the target set for this group's selections. It can only remove targets from the effective set, never add them. |

Locator syntax depends on the source kind: `<kind>/<name>` for Git and directory sources, `<registry-slug>/<kind>/<name>` for Marketplace sources. Supported kinds are `skill`, `command`, `subagent`, `hook`, `mcp`, and `collection`. The same locator may not appear twice in one include list (`v4.duplicate-request`).

Two options can apply to an explicit request:

| Option | Applies to | Effect |
|---|---|---|
| `revision` | Marketplace only | Pins one exact `sha256:<64 hex>` revision of the resource or collection. Invalid on Git and directory sources. |
| `as` | Skills and commands | Renames the generated output. Collections cannot use `as` because they do not materialize. A renamed Git or directory resource is currently rejected at runtime with `v4.runtime-option`. |

Selection happens in one pass: candidates are the union of explicit requests, tag matches, and `all`, then collections expand into their members, then excludes apply. A later explicit include can re-enable a resource that an earlier exclude suppressed, except where `required` protection applies. Excludes in a higher-precedence layer can suppress selections from lower layers, but never required ones. The full merge and conflict rules are in [scopes, selection, and precedence](./scopes-selection-and-precedence.md).

### What selects nothing

Some declarations are valid but select nothing:

- A group with no selection fields (`required: true` alone, `registries` alone, or an empty group) selects nothing.
- Explicit `tags: []` or `all: false` disables selection inherited through an include, and `harnesses: []` selects no targets.
- An `exclude` that matches nothing changes nothing.

## What each half does during a sync

The quick-start example (a Git source named `skills` selecting `skill/frontend-design` for Claude) shows where each map does its work. This is the same pipeline as [how agent-marketplace works](./how-agent-marketplace-works.md), with the steps labeled by the map that drives them:

1. **Load configuration.** Both maps are read and merged across the four layers.
2. **Reconcile the lock.** The source's `ref: main` resolves to one commit, and the commit plus a digest of the source's resources is recorded in `.agents/lock.yaml`. Marketplace pins record exact revisions instead.
3. **Acquire the snapshot.** The exact commit is fetched into the cache and verified against the lock.
4. **Discover.** The snapshot, or the live directory, or the Marketplace catalog, is walked for resources in the source harness's native layout, restricted to `path` when set.
5. **Select.** The group's includes, tags, `all`, collection expansion, and excludes decide which discovered resources survive.
6. **Ingest and compile.** Each selected resource is read into the canonical form, where its content comes from the source, and compiled per target, where its scope and harness set come from the group and its layer.
7. **Materialize.** Files are written into each target harness. Output no longer selected by any group is pruned; content that changed upstream but is still locked stays at the locked revision.

Steps 2 through 4 are driven by the source, step 5 by the group, and steps 6 and 7 by both. The source controls which exact bytes exist; the group controls which of them are installed.

## The differences

- A source is an acquisition declaration, and a group is a selection policy. Deleting a group stops selection but leaves the source acquired and locked (for Git); deleting a source removes the acquisition and breaks any group still bound to its name.
- A source's identity is what it points at (URL, ref, path, layout). A group's identity is the source name it binds to. That is why two sources with different names can share one lock record, and why renaming a source breaks its groups (`v4.undefined-source`) without touching the lock.
- The lock is written from sources; selections are never locked. `sync` re-derives the selected set from the `resources` map every run and prunes whatever is no longer selected.
- One source name has exactly one declaration per configuration root, and one group binds to exactly one source. To treat one repository differently in two ways, declare it as two sources with two names (for example a `stable` and an `edge` ref) and give each its own group.
- Names collide only at the resource level. When two sources deliver the same kind and name, one deterministic winner materializes and a warning is reported; required policy wins first, then the higher-precedence layer, then the alphabetically first normalized origin. See [scopes, selection, and precedence](./scopes-selection-and-precedence.md).

## Words that sound alike

- **Source** (`sources.<name>`) is the acquisition declaration. **Source harness** (`source_harness`) names the file layout of the source's content, not the place it is fetched from. **Configuration layer** is where a source is declared; the layer, not the name, decides machine versus repository scope.
- **Resource** is one installable thing. **Resource group** (`resources.<name>`) is selection policy. **Resource locator** (`skill/frontend-design`) is how a group names one resource. **Collection** is a registry-side bundle that a locator can name but that never materializes.

## Common mistakes

| Mistake | What you see |
|---|---|
| Group key does not match any source name | `v4.undefined-source: resource group references undefined source "…"` |
| Source declares two kinds, or none | `v4.source-discriminator: must declare exactly one of git, directory, marketplace` |
| `ref` or `path` on a directory source, or any Git field on a Marketplace source | `v4.source-field` |
| Same source name declared with different values in one root and its includes | `v4.source-conflict` |
| The same locator twice in one include list | `v4.duplicate-request` |
| A Marketplace group with no matching lock record | `v4.marketplace-lock-required`; Marketplace resolution fails closed |
| Renaming a Git or directory resource with `as` | `v4.runtime-option: installed aliases are not supported by the legacy runtime` |
| Expecting a source to install anything | Nothing materializes; a source only acquires |
| Expecting a directory source to be pinned | It is live; only Git and Marketplace sources lock |

## Related documentation

- [Repository configuration reference](../reference/configuration.md): every field, exhaustively.
- [How agent-marketplace works](./how-agent-marketplace-works.md): the full sync pipeline.
- [Scopes, selection, and precedence](./scopes-selection-and-precedence.md): how layers, excludes, and collisions resolve.
- [Git registries and hosted Marketplaces](./git-registries-and-hosted-marketplaces.md): choosing between the two hosted source kinds.
- [Registry layout reference](../reference/registry-layout.md): where each resource kind is discovered.
- [Collections reference](../reference/collections.md): the registry-side bundle format.
