# Sources and resources

A configuration file (`apiVersion: agent-marketplace/v4`) has two maps, which are [YAML](../glossary.md#yaml) sections of named entries. `sources` says where content comes from and how to fetch it: a Git repository, a folder on your disk, or a hosted [Marketplace](../glossary.md#marketplace). `resources` says what to take from each source. The two are joined by name: the group `resources.skills` selects from the source `sources.skills` and from nothing else. A source on its own installs nothing, and a group cannot exist without a source to select from.

This page explains what each map does, how they differ, and what happens when you change them. The [repository configuration reference](../reference/configuration.md) lists every field.

## The two maps side by side

```yaml
apiVersion: agent-marketplace/v4
harnesses: [claude]
sources:                    # where content comes from
  skills:
    git: https://github.com/anthropics/skills.git
    ref: main
    path: skills
resources:                  # what to take from it, keyed by source name
  skills:
    include:
      - skill/frontend-design
```

| | `sources` | `resources` |
|---|---|---|
| Question it answers | Where does content come from? | What do we take from it? |
| One entry is called | A source | A resource group |
| Keyed by | A name you choose | The name of a declared source |
| Holds | URL or folder, ref, subdirectory, layout | Includes, excludes, tags, protection, target harnesses |
| Lockfile | Git commits and Marketplace pins are recorded | Git and directory selections are never recorded; they are read from config on every run |
| Installs anything alone | No | Yes, through `include`, and through `tags` or `all` for Git and directory sources |
| Valid without the other | Yes, see [declared but never selected](#declared-but-never-selected) | No, a group without a source fails with `v4.undefined-source` |

## What a source is

A source is one named entry under `sources`. The name must start with a letter or digit and may contain letters, digits, `.`, `_`, and `-`. Each entry declares exactly one kind: `git`, `directory`, or `marketplace`. Declaring two kinds or none fails with `v4.source-discriminator`, and a field that belongs to another kind, such as `ref` on a directory source, fails with `v4.source-field`.

A source name is a label inside one configuration root. The layer that declares the source decides where its resources install (machine or repository), and the URL, ref, and path decide what is fetched. The lock and the cache do not store Git or directory source names, so renaming such a source together with its group leaves both as they were. A Marketplace source is different: its name also becomes the endpoint's alias, which `status` shows in the `ALIAS` column and `search --marketplace <alias>` accepts.

A group can only bind to a source declared in the same root, meaning the file itself or any fragment it includes. A source declared in `.agents/config.yaml` is invisible to `.agents/config.local.yaml`, so a personal file that wants to add or exclude resources repeats the source declaration first. [Personal repository overrides](../how-to/use-personal-repository-overrides.md) shows the pattern.

### `git`

```yaml
sources:
  skills:
    git: https://github.com/anthropics/skills.git
    ref: main
    path: skills
    source_harness: claude
```

A Git source is a repository at one URL.

- `ref` is required (`v4.git-ref-required` when missing). It names a branch, tag, or commit, and the first `sync` resolves it to one exact commit.
- That commit goes into the layer's lockfile along with a digest of every resource found under the source. Later syncs keep installing from the locked commit even after the branch moves. Only `update` advances it.
- `path` limits discovery to one subdirectory of the repository.
- `source_harness` names the layout the content is written in, such as `claude` or `cursor`. Leave it out and the CLI detects the layout.

You can declare the same repository under two names, for example a `stable` source on a tag and an `edge` source on `main`, each with its own group. The lock stores one record per URL and ref: two refs give two records, and two names with the same URL and ref share one.

### `directory`

```yaml
sources:
  local:
    directory: ./agent-resources
```

A directory source is a folder on disk.

- The folder is read on every command. It has no ref and no commit, and it never appears in the lock. A `sync` installs whatever the folder holds at that moment.
- In `.agents/config.yaml` and `.agents/config.local.yaml`, the path is relative to the repository root (`directory: .` is the repository itself). In user and organization configs and in included fragments, it is relative to the file that declares it.
- A relative path must stay inside the folder it is resolved against, so `../shared` is rejected. Use an absolute path for a folder outside it.
- `ref` and `path` are rejected. Point `directory` at the subfolder instead.
- `source_harness` works as it does for Git sources.

Because nothing is pinned, a directory source can never be out of date with its lock. The cost is reproducibility: two machines install the same thing only if their folders hold the same files.

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

A Marketplace source is a hosted endpoint that serves resources from many registries.

- Every installed resource is pinned to an exact content revision in the layer lock. When lock evidence is missing or does not match the requested endpoint, registry, resource, and revision, resolution stops with an error.
- Locators have three parts, `<registry-slug>/<kind>/<name>`, because one endpoint serves many registries.
- Groups select through explicit `include` only. The schema accepts `all`, `tags`, `registries`, and group-level `harnesses` on a Marketplace group, but `sync` rejects them with `v4.marketplace-runtime-unsupported`.
- An exclude must name one exact `<registry-slug>/<kind>/<name>`. Wildcards and collections are rejected.
- One layer can hold only one fetching source per endpoint. A second one fails with `v4.marketplace-lock-mismatch`.
- A group that holds only `exclude` turns the source into an exclusion-only override. It fetches nothing, needs no lock record, and hides optional resources that another layer selects from the same endpoint. This form is valid only in the user configuration; anywhere else it fails with `v4.layer-field`.

The [Git registries and hosted Marketplaces explanation](./git-registries-and-hosted-marketplaces.md) covers the trust model, and [installing from a Marketplace](../how-to/install-from-a-marketplace.md) covers the commands.

### Declared but never selected

A source does not need a group. What happens without one depends on the kind:

- A Git source is still resolved, fetched, and locked. Its commit is checked on every sync, but nothing is installed from it.
- A directory source is still read on every run. If the folder holds a layout the CLI recognizes, nothing happens. If it does not, the run fails with an ingestion error.
- A Marketplace source still needs a lock record, just like one with includes. Give it an exclusion-only group if you only want to hide resources.

Remove the declaration if you want nothing fetched at all.

## What a resource is

The word covers two things. A resource is one installable item, such as a skill: the registry ships it, and a sync writes it into your harness. A resource group is one entry under the `resources` map, the selection rules for one source.

### The six resource kinds

| Kind | What it is | What a sync writes for it |
|---|---|---|
| `skill` | A directory with a `SKILL.md` (name, description, optional metadata) | `skills/<name>/` in each target harness |
| `command` | A prompt command in the source harness's native format | The target's command path, such as `.claude/commands/<name>.md` |
| `subagent` | A scoped agent definition | The target's agent path, such as `.claude/agents/<name>.md` |
| `hook` | An event hook or hook group | Entries in the target's hook file, such as `.claude/hooks/hooks.json` |
| `mcp` | An MCP server definition | Entries in the target's MCP file, such as `.mcp.json` |
| `collection` | A named bundle of other resources (`collections/<name>/COLLECTION.yaml`) | Nothing itself; it expands into its members |

Each kind has its own names, so a skill and a command can share a name. Including `collection/onboarding` is shorthand for including every member of that collection. The [registry layout reference](../reference/registry-layout.md) shows where each kind is found in a registry, and [harnesses and output paths](../reference/harnesses-and-output-paths.md) shows where each kind lands for each harness.

### Resource groups

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

| Field | What it does | Source kinds |
|---|---|---|
| `include` | Requests resources by locator, as plain strings or with options. A bare list of locators means the same as `include`. | All |
| `exclude` | Removes matching resources that are not required. `*` matches everything and `kind/*` matches one kind. | All; Marketplace needs exact locators |
| `tags` | Selects resources whose `metadata.tags` contain one of the tags. | Git, directory |
| `all` | Selects every resource of every kind in the source. | Git, directory |
| `registries` | Limits Marketplace `all` and `tags` selection to named registries. Selects nothing by itself. | Marketplace (accepted by the schema, rejected by `sync`) |
| `required` | Protects this group's selections from excludes and from `remove`. A group with only `required: true` selects nothing. | All |
| `harnesses` | Narrows which harnesses receive this group's resources. It can remove harnesses from the target set but never add one. | Git, directory |

A locator names one resource. Git and directory sources use `<kind>/<name>`, and Marketplace sources use `<registry-slug>/<kind>/<name>`. Listing the same locator twice in one include list fails with `v4.duplicate-request`.

An include can carry two options:

| Option | Effect |
|---|---|
| `revision` | Pins one exact `sha256:<64 hex>` revision. Marketplace only; Git and directory sources reject it. |
| `as` | Installs the resource under a different name. It works for Marketplace resources other than collections. On Git and directory sources, any name other than the resource's own fails with `v4.runtime-option`. |

### How a group picks resources

The candidates are everything named by `include`, matched by `tags`, or covered by `all`. Collections then expand into their members, and excludes remove anything that is not required.

Within one file, excludes always apply after includes, whatever order you write the keys in. If one file both includes and excludes `skill/a`, `skill/a` is excluded. Across an include tree the order matters: a fragment merges below the file that includes it, so an include in the including file brings back a resource that the fragment excluded.

Excludes from a higher layer also hide resources selected by a lower layer, with two limits. Required resources are never hidden, and repository or personal-local excludes never reach resources installed at machine scope. [Scopes, selection, and precedence](./scopes-selection-and-precedence.md) has the full rules.

A few declarations are valid but select nothing: an empty group, a group with only `required: true` or only `registries`, and an exclude that matches nothing. In an including file, `tags: []` or `all: false` switches off tag or all selection inherited from a fragment, and `harnesses: []` means no harness receives the group's resources.

## What happens during a sync

The steps below use the quick-start example, a Git source named `skills` with one skill selected for Claude, and show where each map does its work. [How agent-marketplace works](./how-agent-marketplace-works.md) describes the same pipeline in more detail.

1. Load configuration. Both maps are read and merged across the four layers.
2. Check the lock. On the first run, or after `ref` changes, `ref: main` resolves to one commit, and that commit is written to `.agents/lock.yaml` with a digest of the resources under the source. On later runs the locked commit is reused. Marketplace sources record pinned revisions instead.
3. Fetch the snapshot. The locked commit is downloaded into the cache and checked against the lock.
4. Discover. The snapshot is searched for resources in the source's layout, limited to `path` when set. A directory source is read from disk here, and Marketplace resources come from their pinned revisions.
5. Select. The group's includes, tags, `all`, collections, and excludes decide which of the discovered resources to install.
6. Compile. Each selected resource is read from the source and converted for each target harness. The group and the layer that declares it decide which harnesses and which scope.
7. Write. Files land in each harness's native layout. Output the CLI generated earlier that no group selects any more is removed. Content that changed upstream stays at the locked version.

The source drives steps 2 to 4 and decides which exact files exist. The group drives step 5 and decides which of those files you get. Steps 6 and 7 use both.

## What happens when you change the configuration

A Git lock covers every resource under the source, whether a group selects it or not, so most group edits leave the lock alone.

| You change | Next `sync` | Lock |
|---|---|---|
| Add a locator to a Git group's `include` | Installs it from the commit already locked | Unchanged |
| Remove a locator, or delete the whole group | Removes the generated files | Unchanged; the source stays locked |
| Delete the source and its group | Removes the generated files | The source's record is dropped |
| Delete the source but keep its group | Fails with `v4.undefined-source` | Unchanged |
| Change `ref` | Resolves the new ref and installs from that commit | Rewritten for the new ref |
| Someone pushes to the branch named in `ref` | Nothing; the locked commit stays installed | Unchanged |
| Run `agent-marketplace update` | Moves to the ref's current commit; new resources upstream appear in `list`, and the ones your group selects are installed | Commit advanced |
| Edit files in a directory source | Installs the new content | Not involved |
| Rename a Git or directory source and its group together | Nothing | Unchanged; the lock stores URLs and refs, not names |

## Editing sources and groups from the command line

You can edit both maps by hand, or let `install` and `remove` write them.

`install <name> --from <url>` adds the source if it is missing, then adds `<kind>/<name>` to its group's `include`. The command names the source after the last segment of the URL, minus any `.git` suffix, so `https://github.com/example-org/team-skills.git` becomes `team-skills`. If another source already has that name, it tries `team-skills-2`, then `team-skills-3`. If the segment is not a valid source name, it falls back to `registry`. Running the command again with the same URL, ref, and path reuses the existing source.

The kind comes from the URL. A local path that does not end in `.git`, such as `./resources`, `/srv/resources`, `~/resources`, or a `file://` path without `.git`, becomes a `directory` source. Anything else, including a local bare repository ending in `.git`, becomes a `git` source that uses `--ref` when you pass one.

Without a resource name, `install --from <url>` writes a group with `all: true`.

`remove <name>` edits only the group. It drops the include and may add an `exclude` to keep the resource out, for example when `all` or `tags` would select it again. The source stays declared, and a Git source stays locked. `remove --from <url>` without a name deletes both the source and its group.

Both commands take `--scope user`, `--scope repo`, or `--scope local` to choose which configuration file they edit. The [command-line reference](../reference/cli.md) lists every flag.

## Terms that are easy to mix up

The word "source" means different things in different places:

- In the configuration file, a source is an entry under `sources`.
- `source_harness` is the layout the source's files are written in, such as `claude`. It has nothing to do with where the files come from.
- In `status`, the `SOURCE` column is the configuration layer that declared the item, such as `REPO` or `ORG`. The `REGISTRY` column shows the source URL.
- In `search`, the `SOURCE` column is the Marketplace registry slug, the first part of a three-part locator.

Your own source names barely appear in command output. `list` has a `REGISTRY` column with a short name taken from the URL, so two sources for the same repository at different refs appear under one registry name. For a Git source, `list` shows what the locked commit contains, and a resource added upstream appears only after `update`. The one place a source name shows up is the `ALIAS` column for Marketplace sources in `status`.

On the resource side, a resource is one installable item, a resource group is the selection rules under `resources.<name>`, a locator such as `skill/frontend-design` is how a group names one resource, and a collection is a bundle a locator can name that never installs as itself.

## Common mistakes

| Mistake | What you see |
|---|---|
| Group name does not match any source | `v4.undefined-source: resource group references undefined source "…"` |
| A personal-local file excludes from a source declared only in the committed file | `v4.undefined-source`; repeat the source declaration in the local file |
| Git source without `ref` | `v4.git-ref-required` |
| Source declares two kinds, or none | `v4.source-discriminator: must declare exactly one of git, directory, marketplace` |
| `ref` or `path` on a directory source, or a Git field on a Marketplace source | `v4.source-field` |
| One source name declared with different values in a file and its includes | `v4.source-conflict` |
| The same locator twice in one include list | `v4.duplicate-request` |
| Including and excluding one resource in the same file | The exclude wins |
| `all`, `tags`, `registries`, or `harnesses` on a Marketplace group | `v4.marketplace-runtime-unsupported` |
| Exclusion-only Marketplace group outside the user configuration | `v4.layer-field` |
| A Marketplace group without a matching lock record | `v4.marketplace-lock-required` or `v4.marketplace-lock-mismatch` |
| Renaming a Git or directory resource with `as` | `v4.runtime-option: installed aliases are not supported by the legacy runtime` |
| Expecting a new upstream commit after `sync` | `sync` keeps the locked commit; run `update` |
| Expecting a source to install something by itself | Nothing is installed until a group selects it |
| Expecting a directory source to be pinned | It is read live; only Git and Marketplace sources lock |

## Related documentation

- [Repository configuration reference](../reference/configuration.md): every field.
- [How agent-marketplace works](./how-agent-marketplace-works.md): the full sync pipeline.
- [Scopes, selection, and precedence](./scopes-selection-and-precedence.md): how layers, excludes, and name collisions resolve.
- [Git registries and hosted Marketplaces](./git-registries-and-hosted-marketplaces.md): choosing between the two hosted source kinds.
- [How to use personal repository overrides](../how-to/use-personal-repository-overrides.md): redeclaring a source in `.agents/config.local.yaml`.
- [Registry layout reference](../reference/registry-layout.md): where each resource kind is found.
- [Collections reference](../reference/collections.md): the bundle format.
