# User journeys

This map covers using resources through the CLI, terminal UI, and hosted Marketplace browser, from finding one to updating or removing it. Publishing resources and running a Marketplace have separate guides.

```text
Find → Inspect → Choose scope and harnesses → Install → Use
                                                ↑        ↓
                                             Recover ← Check
                                                         ↓
                                                  Update or remove
```

## Before following a journey

[Install the CLI](../install.md), then choose your coding harnesses with `agent-marketplace init` or the TUI's first-run prompt. You can browse with `list` before choosing harnesses, but installation and sync need that choice.

::: warning Check your configuration version
V4 is the canonical configuration schema, and `init` creates a v4 user config directly; interactive CLI startup and the TUI also offer to migrate a legacy user config (the user-personal root only). However, the commands and TUI actions that change resource selections still require v2/v3 files: the CLI's `install` and `remove` operations, endpoint registration, and TUI resource toggles fail with `v4.writer-opt-in` if they need to edit a v4 file. The TUI's harness modal (`H`) can still edit the target harness list of a v4 config.

For v2/v3, follow the command and TUI steps below. For v4 Git and directory sources, edit `sources` and `resources` directly, then run `sync`. Hosted v4 requests also need matching verified records in the layer's lockfile. Adding a source and digest by hand is not enough to install one. Read [maintaining and migrating configuration](../how-to/migrate-configuration.md) before switching to v4.
:::

Choose where a resource belongs before installing it:

| Intent | CLI scope | Configuration | Output |
|---|---|---|---|
| Use it across my projects | `--scope user` | `~/.agents/config.yaml` | Machine harness roots |
| Share it with this repository's team | `--scope repo` | `.agents/config.yaml` | Repository harness roots |
| Use it only in my checkout | `--scope local` | `.agents/config.local.yaml` | Repository harness roots; private selection |

Resource installation defaults to `repo`; registering a Marketplace endpoint defaults to `user`. Local scope changes which resources your checkout selects. It uses the repository's harness directories, not a separate installation directory. See [scope and precedence](./scopes-selection-and-precedence.md).

## 1. Find, inspect, and install a resource

Start with a Git registry URL, a Marketplace link, or a source already in your config.

1. Use `list` for Git/directory registries, `list --from <git-url>` for an unconfigured Git registry, or `search <query>` for hosted Marketplaces. You can also browse configured sources in the [TUI](../reference/tui.md) or use [search and filters in the browser](../how-to/browse-marketplace-resources.md).
2. Open resource files with `enter` in the TUI, or read the browser's Content, Security, Revisions, and Registry tabs. Check the resource kind, prerequisites, source, and license. An automated scan cannot guarantee that a resource is safe.
3. Choose a scope from the table above and check `status` to see which harnesses will receive the resource. Not every harness supports every resource kind; check diagnostics and the [harness reference](../reference/harnesses-and-output-paths.md).
4. Follow the [Git tutorial](../tutorials/install-your-first-skill.md) or [Marketplace tutorial](../tutorials/install-your-first-marketplace-skill.md) to install it. The browser's copied command pins the revision you're viewing, without requiring you to register the endpoint first. If the CLI reports Marketplace warnings or active capabilities, review the verified preview before accepting. The prompt cannot approve a blocked or revoked revision.
5. Run `status` and `sync --check`, then open or reload your coding harness in the project. Agent Marketplace installs the resource; you use it through your harness.

Check the resource's scope and harness output in `status`. The drift check should exit `0`.

If a name is ambiguous, add `--kind` or use a locator that includes the source. For a Marketplace local-name conflict, use `--as`. Follow the resource's instructions to set up prerequisites: Agent Marketplace reports them but does not install them. See [hosted installation troubleshooting](../how-to/install-from-a-marketplace.md#troubleshooting) for other failures.

## 2. Join a repository that already has resources

When a teammate has committed the repository config and its generated lock, you can reproduce that setup:

1. Clone the repository and run commands inside it.
2. Run `agent-marketplace sync --locked`. This fetches the exact locked Git snapshots and generates harness output without rewriting the config or locks.
3. Run `agent-marketplace status` to see the harness targets and config layers, then `agent-marketplace sync --check` to check for drift.
4. Open your coding harness. Use [personal repository overrides](../how-to/use-personal-repository-overrides.md) for additions or exclusions that should only affect your checkout.

Your checkout should now have the team's resources at the locked revisions. Resources from your user and organization configs still apply, so you may also see machine-wide resources that are not in the repository config.

If the committed lock is missing or stale, someone needs to update it in the repository. On a fresh CI runner, run `sync --locked` before `sync --check`; the check cannot download snapshots or create output. See [reproducibility](../how-to/keep-resources-reproducible.md) and [drift recovery](../how-to/troubleshoot-sync-drift.md).

## 3. Build a personal or shared setup

Once you have a resource working, you can add more resources or harnesses and decide which parts of the setup to share.

1. Add commands, subagents, hooks, or MCP servers alongside your skills. Use [tags, collections, or registry bundles](../how-to/install-resource-bundles.md) to select a group.
2. Choose which scope owns each selection. Commit the repository config and lock for teammates, and keep local config and its lock ignored. Generated output should not be the source of truth in Git.
3. [Configure harnesses](../how-to/configure-multiple-harnesses.md). The repository's target list overrides the user's list, and each source can further restrict its targets. In the TUI, use `H` to choose the user or project harness list.
4. Sync and check the result. Read shared-configuration warnings before using new hooks or MCP servers. Codex repository changes may need a separate trust review in Codex. Agent Marketplace does not grant that trust for you.

Check that each harness has the output it supports and that another checkout can reproduce the shared setup without your personal config.

For local resources, you can use [live local directories](../how-to/use-a-local-registry.md), which are not Git-locked. If symlinks don't suit your environment, choose [file materialization](../how-to/write-regular-files-instead-of-symlinks.md). To copy an existing harness layout without setting up ongoing sync, use [one-shot conversion](../how-to/convert-a-harness-layout.md).

## 4. Check or update an existing setup

Checking for drift and installing a new upstream revision are different operations:

| Intent | Action | What changes |
|---|---|---|
| See what is selected and where it came from | `status` | Leaves generated output unchanged |
| Detect drift | `sync --check` | Read-only; does not download missing cached content |
| Restore output using committed locks | `sync --locked` | Cache and generated output; not config or locks |
| Apply selection changes | `sync` | Output and any writable locks that are stale or missing |
| Advance Git revisions and install them | `update --scope repo` | Selected scope's Git locks and generated output |
| Advance Git locks separately | `lock --upgrade --scope repo` | Locks; run `sync` afterward to apply output |

An ordinary sync keeps a healthy Git lock at its recorded revision, even if the branch has moved. `update` defaults to all writable scopes, so pass a scope to limit it to the repository. The TUI's `u` action upgrades Git locks across writable scopes and syncs the output. To reload state without upgrading, use `r`.

Once the revisions you wanted are installed, review the config/lock diff and run `sync --check`. When the check passes, commit shared changes together. Legacy hosted mirror pins can be updated. Updates to index-only and organization-required pins have restrictions. General hosted v4 acquisition/update and snapshots for dynamic selectors are not implemented. See [the command reference](../reference/cli.md#update).

## 5. Remove something without changing everyone else's setup

To remove a resource or opt out of an optional team selection:

1. Check `status` for the source and scope that select it. The same name can exist in both machine and repository scopes.
2. Follow [remove or override resources](../how-to/remove-or-override-resources.md), passing a scope when needed. Removal deletes a direct selection or adds an exclusion for a resource selected indirectly.
3. If a hosted collection also selects the resource, removing its direct selection leaves the collection's selection in place. Remove the collection selector to stop selecting its members.
4. Check `status` and `sync --check` again. Confirm that you removed the selection you meant to remove and kept the others. Another scope or selection may still keep the resource installed.

Required resources cannot be excluded, and local overrides cannot suppress machine-wide installations. Deleting generated files by hand only removes them until the next sync. To remove the whole setup, follow the [safe removal guide](../how-to/remove-agent-marketplace-safely.md), starting with its dry run.

## 6. Recover, work offline, or clean up

| Situation | Next step | What to watch for |
|---|---|---|
| Missing output or a failed drift check | [Troubleshoot sync drift](../how-to/troubleshoot-sync-drift.md) | Read diagnostics before rewriting config or locks |
| A handwritten hook/MCP entry conflicts with generated output | [Repair shared configuration](../how-to/repair-owned-shared-configuration.md) | `doctor --repair --yes` can adopt exact matches; removing or replacing entries still requires confirmation |
| A machine-wide install needs recovery | [Back up and restore](../how-to/back-up-and-restore-machine-resources.md) | These backups cover machine output, not repository history |
| No network connection | `sync --offline` in the [CLI reference](../reference/cli.md#sync) | Needs trusted cached content and cannot check for new revocations |
| Private Git acquisition fails | [Configure private Git registries](../how-to/configure-private-git-registries.md) | Use the supported Git authentication path, not credentials in committed URLs |
| Cache consumes too much space | [Clean the cache safely](../how-to/clean-the-cache-safely.md) | Preview with `gc --dry-run`; the shared cache may contain resources used by other repositories that this check does not cover |
| Review or change data collection | [Configure telemetry](../how-to/configure-telemetry.md) | Organization policy may limit your choices; inspect diagnostic payloads before sharing |
| Stop using Agent Marketplace | [Remove it safely](../how-to/remove-agent-marketplace-safely.md) | Teardown and removing the binary are separate steps |

## Coverage and current limits

For command flags and less common options, use the [CLI reference](../reference/cli.md) and [TUI reference](../reference/tui.md). To set up shell completion, see [completion](../reference/cli.md#completion).

Harness support and these limits determine which parts of the workflows are available:

- In the browser, you can inspect resources and copy install commands. Run those commands locally to install them in your harness.
- TUI Marketplace discovery shows at most 50 results per endpoint/query, without pagination. Narrow the query or use CLI search for more.
- V4 supports reading configuration and explicit migration. Resource editors and general hosted acquisition do not yet support it.
- Agent Marketplace does not install prerequisites for you. Collections and tags group selections but do not resolve dependencies.
- Publishing resources, running a Marketplace, and setting organization policy are outside these consumer journeys.
