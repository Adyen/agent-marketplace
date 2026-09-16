# How agent-marketplace works

`agent-marketplace` is a package manager for AI agent resources. It reads committed or personal configuration, discovers resources in Git registries or a hosted Marketplace, and writes them into the native layout of each coding harness you use.

## Core pieces

- **Configuration layers** decide which registries and resources are selected. Layers stack from organization policy down to personal-local overrides. See the [configuration layers reference](../reference/configuration-layers.md) for the exact merge order.
- **Git locks** record the exact commit and source manifest for every remote Git requirement, so a `sync` recreates the same bytes on any machine.
- **Harness compilation** translates a canonical intermediate representation into each target's native files. Skills, commands, subagents, hooks, and MCP servers each become the files the harness expects.
- **Materialization** writes the native output to the repository or user machine. Configuration is the source of truth; the generated files are recreated from configuration and locks.

## Data flow

1. Load the four configuration layers: organization, user, repository, and repository-personal.
2. Reconcile layer-owned locks so every configured Git requirement resolves to an exact commit.
3. Acquire the exact snapshots for the resolved commits.
4. Discover registry resources and apply selectors, tags, collections, excludes, and precedence rules.
5. Compile each selected resource for the effective target harness set.
6. Materialize native files and prune stale generated output.

## Where state lives

- **Committed**: `.agents/config.yaml` and `.agents/lock.yaml` go in source control.
- **Personal**: `~/.agents/config.yaml` and `~/.agents/lock.yaml` stay on the machine.
- **Local overrides**: `.agents/config.local.yaml` and `.agents/lock.local.yaml` are gitignored.
- **Cache**: `~/.cache/agent-marketplace/` is disposable. Deleting it does not change configuration, locks, or durable ownership state, but later `sync` may need to reacquire exact commits from the remote. Reacquisition can fail offline or when the remote no longer serves the locked commit.
- **Ownership state**: Devin and Codex aggregate ownership lives in durable XDG state outside the cache, so cache deletion does not affect owned shared configuration.

## Related documentation

- [Configuration layers reference](../reference/configuration-layers.md)
- [Scopes, selection, and precedence](../explanation/scopes-selection-and-precedence.md)
- [Reproducibility and lockfiles](../explanation/reproducibility-and-lockfiles.md)
- [Native harness materialization](../explanation/native-harness-materialization.md)
