# Scopes, selection, and precedence

`agent-marketplace` combines configuration from many sources. Understanding how it merges layers and resolves conflicts helps predict which resources are installed and which excludes win.

## Configuration scopes

Resources materialize into one of two scopes:

- **Machine scope** comes from organization or user configuration. It writes into user home directories such as `~/.claude/skills/` or `~/.config/devin/`.
- **Repository scope** comes from committed or personal-local repository configuration. It writes into the project directory next to `.agents/config.yaml`.

The same kind and name can exist in both scopes. They are resolved independently, except that `remove` without `--scope` reports an ambiguity.

## Selection steps

For each registry entry, the CLI computes the selected resources per kind:

1. Inherit top-level defaults for that kind and for `tags`.
2. Replace them with the registry entry's own selector or `tags`.
3. If the entry has `required: true` or `default: true` and has no explicit selectors or tags, auto-select all resources of that kind.
4. Add resources that match `tags`.
5. Apply per-kind `exclude`. `required` resources are immune.
6. Apply `exclude_all` if set.

## Precedence between layers

Higher layers override lower layers, with one exception: target harnesses use project-first precedence. The winner for most conflicts is determined by:

1. `required: true`
2. Higher configuration source or file precedence
3. Alphabetically first normalized registry URL
4. Stable configuration order

## Exclusions

Excludes from a higher layer suppress lower-layer non-required resources when the scope boundary permits. `config.local.yaml` per-kind excludes apply to repo-scoped, non-required resources. Machine-scoped and required resources are not suppressed by repository or local excludes.

## Collections

Collections expand into synthetic registry entries before resolution. They respect the same exclusion rules. Removing a collection member records an `exclude` in the active scope while keeping the collection selected.

## Related documentation

- [Configuration layers reference](../reference/configuration-layers.md)
- [How to remove or override resources](../how-to/remove-or-override-resources.md)
- [How to use personal repository overrides](../how-to/use-personal-repository-overrides.md)
