# Sources and resources

A v4 configuration file (`apiVersion: agent-marketplace/v4`) has two maps. `sources` says where content comes from: a Git repository, a folder on your disk, or a hosted [Marketplace](../glossary.md#marketplace). `resources` says what to take from each source. The two are joined by name: the group `resources.skills` selects from the source `sources.skills` and from nothing else. A source on its own installs nothing, and a group cannot exist without a source to select from; a group that names no declared source fails with `v4.undefined-source`.

## Acquisition versus selection

The `sources` map answers one question: where does content come from? Each entry names one kind, `git`, `directory`, or `marketplace`, plus the location details that kind needs, such as a URL, a `ref`, or a `path` inside a repository. The `resources` map answers a different question: which of the discovered resources do we install? A group selects resources with `include` lists, `tags`, or `all`, narrows them with `exclude` and `harnesses`, and protects them with `required`.

Editing a group changes only what gets installed. Editing a source changes what can be found at all. Deleting a source while its group stays behind leaves the group with nothing to select, which is a configuration error.

A group can only bind to a source declared in the same configuration root, meaning the file itself or any fragment it includes. A source declared in `.agents/config.yaml` is invisible to `.agents/config.local.yaml`, so a personal file that wants to add or exclude resources repeats the source declaration first. [Personal repository overrides](../how-to/use-personal-repository-overrides.md) shows the pattern.

## The three source kinds

### `git`: pinned to one commit

A Git source is a repository at one URL and one `ref` (a branch, tag, or commit). The first `sync` resolves the ref to one exact commit and records it in the layer's lockfile, with digests of the resources found there. Later syncs keep installing from that commit even after the branch moves. An explicit `update` or `lock --upgrade` advances the lock. Changing the configured `ref` makes the next writable `sync` resolve it and rewrite the lock record. A locked commit stays installable while the Git server still retains it. You can declare one repository under two names, such as a `stable` source on a tag and an `edge` source on `main`, each with its own group.

Trade-off: you get reproducibility and an audit trail, at the cost of a lock to maintain and a first-time network fetch.

### `directory`: read live, never pinned

A directory source is a folder on disk. It has no ref and no commit, never appears in a lock, and is read on every run, so a `sync` installs whatever the folder holds at that moment. Two machines install the same thing only if their folders hold the same files.

Trade-off: no lock and no fetch, at the cost of reproducibility. It suits resources you author or iterate on before publishing.

### `marketplace`: pinned content revisions with policy

A Marketplace source is a hosted endpoint that serves resources from many registries. Every installed resource is pinned to an exact content revision in the layer lock. A strict read that cannot find or verify matching evidence reports the gap instead of guessing. Online commands may acquire the evidence for a missing writable lock. Groups select through explicit `include` only: `sync` rejects the broader selectors the schema accepts, such as `all` and `tags`, for Marketplace groups. In return, the operator can curate registries, apply license and content policy before anyone installs, and revoke content later.

### Declared but never selected

A source does not need a group. Without one, a Git source is still resolved and fetched (and locked where the layer keeps a lock), a directory source is still read on every run, and a Marketplace source still needs matching lock evidence before its pins are used. Nothing is installed until a group selects it. Remove the declaration if you want nothing fetched at all.

## What a resource is

The word covers two things. A resource is one installable item, such as a skill, command, subagent, hook, or MCP server: the registry ships it, and a sync writes it into your harness. A collection is different: a named bundle a group can select that expands into its member resources and never lands on disk itself. A resource group is one entry under the `resources` map, the selection rules for one source. A locator such as `skill/frontend-design` is how a group names one resource; Marketplace locators add a registry slug because one endpoint serves many registries.

## How a group picks resources

The candidates are everything named by `include`, matched by `tags`, or covered by `all`. Collections then expand into their members, and excludes remove anything that is not `required`. Within one file, excludes always apply after includes, whatever order the keys are written in; across an include tree, a fragment merges below the file that includes it. An include can carry a rename option (`as`) and an exact `revision` pin where the source kind supports them.

A few declarations are valid but select nothing: an empty group, a group with only `required: true`, or an exclude that matches nothing. [Scopes, selection, and precedence](./scopes-selection-and-precedence.md) covers how excludes and collisions resolve across configuration layers.

## What locks mean

Locks pin the remote kinds, not the local one, and only where a lock may exist: every writable layer records its own, while an organization layer whose administrator deploys no lock leaves its Git sources following their refs unpinned. A Git source's lock records the commit its ref resolved to and digests of the resources under it; a Marketplace source's lock records the exact content revisions. The lock covers every resource under a Git source whether a group selects it or not, so most group edits leave it unchanged, while changing a `ref` rewrites it. A directory source is never locked because it is read live.

Because a Git lock stores URLs and refs rather than your source names, renaming a Git or directory source together with its group leaves the lock and generated files unchanged. A Marketplace source name is different: it also becomes the endpoint's alias, which `status` and `search` use.

## Related documentation

- [Repository configuration reference](../reference/configuration.md): every field.
- [Diagnostics reference](../reference/diagnostics.md): every error code.
- [How agent-marketplace works](./how-agent-marketplace-works.md): the full sync pipeline.
- [Reproducibility and lockfiles](./reproducibility-and-lockfiles.md): the lock format and update commands.
- [Install and remove](../reference/cli.md): commands that edit both maps for you.
- [Scopes, selection, and precedence](./scopes-selection-and-precedence.md): how layers, excludes, and name collisions resolve.
- [Git registries and hosted Marketplaces](./git-registries-and-hosted-marketplaces.md): choosing between the two hosted source kinds.
- [How to use personal repository overrides](../how-to/use-personal-repository-overrides.md): redeclaring a source in `.agents/config.local.yaml`.
