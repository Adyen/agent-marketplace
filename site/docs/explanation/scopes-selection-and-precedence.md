# Scopes, selection, and precedence

`agent-marketplace` can read up to four configuration files at once: one from your organization, one in your home folder, one committed to the project, and one personal file for this checkout. This page explains where each file's resources are installed, how a resource group picks resources, and which file wins when two of them disagree. Knowing this lets you predict what a `sync` will install and which exclude takes effect.

## Where resources are installed

Every resource lands in one of two [scopes](../glossary.md#scope), depending on which file selected it:

- Machine scope covers resources from the organization file (`/etc/agent-marketplace/config.yaml`) and your user file (`~/.agents/config.yaml`). They go into folders in your home directory, such as `~/.claude/skills/` or `~/.config/devin/`, so every project on the machine sees them.
- Repository scope covers resources from the committed project file (`.agents/config.yaml`) and the personal-local file (`.agents/config.local.yaml`). They go into the project folder, next to `.agents/`, so only that project sees them.

The same kind and name can exist in both scopes, for example a `review` skill in your user file and another `review` skill in the project. The CLI handles each scope separately and installs both. `remove review` without `--scope` then reports that the name is ambiguous and asks you to choose.

## How a resource group picks resources

A resource group is one entry under `resources:`, and it selects from the source with the same name. For each group, the CLI works out the selection in this order:

1. It collects candidates: every locator under `include`, every resource whose `metadata.tags` contain one of the group's `tags`, and every resource in the source when `all: true` is set.
2. It replaces each included collection with its members.
3. It removes whatever `exclude` matches. `*` matches everything and `kind/*` matches one kind, such as `skill/*`. Resources protected by `required: true` stay.

A group with only `required: true` and no include, tags, or `all` selects nothing. Within one file, excludes apply after includes regardless of the order you write them in. [Sources and resources](./sources-and-resources.md#how-a-group-picks-resources) covers the fields in detail, including how included fragments merge.

Every existing consumer root must declare exact v4. There is no legacy reader or implicit select-all interpretation of `required: true`; use explicit selections and, for Git or directory sources, `all: true` when you intend to select everything. [Convert older configurations with an older released migrator before upgrading](../how-to/migrate-configuration.md).

## Which file wins

Most settings follow one rule: a higher file overrides a lower one, in the order organization, user, repository, personal-local.

The list of target harnesses is the exception. The CLI takes it from the project file when that file sets top-level `harnesses:`, otherwise from your user file, and falls back to the organization file. A group's own `harnesses:` can narrow that list but never add to it.

When two sources provide a resource with the same kind and name in the same scope, only one can be installed. The CLI picks the winner with these rules, in order, and prints a warning naming it:

1. A resource marked `required: true` wins.
2. A resource from a higher-priority configuration file wins.
3. The source whose normalized URL sorts first alphabetically wins.
4. The source listed first in the configuration wins.

Hooks are the exception. Same-named hooks from different sources are all installed side by side. Selecting the same hook twice from one source still installs it once.

## How excludes cross files

An exclude in a higher file can hide an optional resource that a lower file selected. In a v4 file, the group that holds the exclude must bind to a source declared in that same file or in a fragment it includes, so you repeat the source declaration first. Two limits apply:

- Required resources are never hidden.
- Excludes in the repository or personal-local file affect only resources installed in the repository. They cannot hide anything the organization or user file installs in your home folder.

In practice, `.agents/config.local.yaml` is where you turn off a resource the team's committed file selects, without changing the committed file.

## Collections

A collection is a named bundle of resources. Before the selection rules run, the CLI replaces each selected collection with its members, and the same exclude rules then apply to each member. Running `remove` on one member adds an `exclude` for it in the file you are editing, and the collection stays selected.

## Related documentation

- [Configuration layers reference](../reference/configuration-layers.md)
- [How to remove or override resources](../how-to/remove-or-override-resources.md)
- [How to use personal repository overrides](../how-to/use-personal-repository-overrides.md)
