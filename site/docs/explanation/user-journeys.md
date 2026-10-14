# User journeys

This page maps the common paths through the product for people who use resources: finding one, installing it, sharing it with a team, updating it, and removing it. Each journey gives the CLI, terminal UI (TUI), and Marketplace browser steps where they apply. Publishing resources and running a Marketplace have their own guides.

```text
Find → Inspect → Choose scope and harnesses → Install → Use
                                                ↑        ↓
                                             Recover ← Check
                                                         ↓
                                                  Update or remove
```

## Before following a journey

[Install the CLI](../install.md), then choose your coding [harnesses](../glossary.md#harness) with `agent-marketplace init` or with the prompt the TUI shows on first launch. You can browse with `list` before you choose harnesses, but installing and syncing need that choice.

::: warning Check your configuration version
The current configuration format is v4 (`apiVersion: agent-marketplace/v4`). `init` and the TUI's first-run prompt create v4 files. On a v4 file, these edits work:

- `install` and `remove` by name, including `--from` for a Git or directory registry that isn't configured yet;
- `install --tag` and `install --all`;
- registering a Marketplace and installing Marketplace resources; and
- TUI toggles for Git and directory sources, and the TUI harness modal (`H`).

Every existing root, including organization and personal-local, must declare exact v4. Included fragments may omit the version or declare exact v4. Versionless roots fail with `v4.api-version-required`; explicit unsupported or invalid versions fail with `v4.api-version`, naming the file and field. There is no shipped `migrate` command, startup prompt, TUI migration modal, or legacy fallback. [Convert writable roots with an older released migrator before upgrading](../how-to/migrate-configuration.md); administrators convert organization configuration externally. Conversion can refuse includes, unrepresentable legacy input, or missing verified Marketplace evidence, and changing only the version string is insufficient.

Some Marketplace cases are not supported: acquiring content offline when the organization lockfile is absent, rebinding hosted pins, and recording snapshots for dynamic selectors. When the CLI can't verify a pin, it refuses and changes nothing.
:::

Choose where a resource belongs before you install it. The [scope](../glossary.md#scope) decides which configuration file records it:

| Intent | CLI scope | Configuration | Output |
|---|---|---|---|
| Use it across my projects | `--scope user` | `~/.agents/config.yaml` | Machine harness roots |
| Share it with this repository's team | `--scope repo` | `.agents/config.yaml` | Repository harness roots |
| Use it only in my checkout | `--scope local` | `.agents/config.local.yaml` | Repository harness roots; private selection |

Installing a resource defaults to `repo`. Registering a Marketplace defaults to `user`. Local scope changes which resources your checkout selects, and writes them into the repository's harness folders; there is no separate installation folder. See [scopes, selection, and precedence](./scopes-selection-and-precedence.md).

## 1. Find, inspect, and install a resource

Start from a Git registry address, a Marketplace link, or a source that is already in your configuration.

1. Find the resource. Use `list` for configured Git and directory registries, `list --from <git-url>` for a Git registry you haven't configured, or `search <query>` for hosted Marketplaces. You can also browse configured sources in the [TUI](../reference/tui.md) or use [search and filters in the browser](../how-to/browse-marketplace-resources.md).
2. Inspect it. In the TUI, press `enter` on a row to open the resource's files. In the browser, read the **Content**, **Security**, **Revisions**, and **Registry** tabs. Check the resource kind, prerequisites, source, and license. An automated scan can't prove that a resource is safe.
3. Choose a scope from the table above, and run `status` to see which harnesses will receive the resource. Not every harness supports every resource kind; check the diagnostics and the [harness reference](../reference/harnesses-and-output-paths.md).
4. Install it by following the [Git tutorial](../tutorials/install-your-first-skill.md) or the [Marketplace tutorial](../tutorials/install-your-first-marketplace-skill.md). The install command you copy from the browser pins the revision you are looking at, and it works without registering the Marketplace first. If the CLI or TUI reports security findings, review the preview before you accept. Capabilities alone don't need consent, and nothing can approve a blocked or revoked revision.
5. Run `status` and `sync --check`, then open or reload your harness in the project. `agent-marketplace` installs the resource; you use it through your harness.

`status` should list the resource with the scope and harness you chose, and `sync --check` should exit with `0`.

If a name matches more than one resource, add `--kind` or use a [locator](../glossary.md#locator) that includes the source. If a Marketplace resource's name clashes with one you already have, install it under another name with `--as`. `agent-marketplace` reports prerequisites but doesn't install them, so follow the resource's own setup instructions. For other failures, see [hosted installation troubleshooting](../how-to/install-from-a-marketplace.md#troubleshooting).

## 2. Join a repository that already has resources

When a teammate has committed the repository configuration and its [lockfile](../glossary.md#lockfile), you can reproduce their setup:

1. Clone the repository and run the commands inside it.
2. Run `agent-marketplace sync --locked`. It downloads the exact commits recorded in the lockfile and writes the harness files, without changing the configuration or lockfiles.
3. Run `agent-marketplace status` to see the harness targets and configuration layers, then `agent-marketplace sync --check` to check for [drift](../glossary.md#drift).
4. Open your harness. For additions or exclusions that should affect only your checkout, use [personal repository overrides](../how-to/use-personal-repository-overrides.md).

Your checkout now has the team's resources at the recorded revisions. Resources from your personal and organization configurations still apply, so you may also see machine-wide resources that aren't in the repository configuration.

Everyone who reads the committed lockfile, including CI, needs `agent-marketplace` v4.0.9 or newer. If the committed lockfile is missing or out of date, someone has to update it in the repository. On a fresh CI runner, run `sync --locked` before `sync --check`, because the check doesn't download anything or write files. See [reproducibility](../how-to/keep-resources-reproducible.md) and [drift recovery](../how-to/troubleshoot-sync-drift.md).

## 3. Build a personal or shared setup

Once one resource works, you can add more resources or harnesses and decide which parts of the setup to share.

1. Add commands, subagents, hooks, or MCP servers next to your skills. To select a group at once, use [tags, collections, or registry bundles](../how-to/install-resource-bundles.md).
2. Decide which scope owns each selection. Commit the repository configuration and lockfile for your teammates. Keep the personal-local configuration and its lockfile out of Git. The generated harness files don't belong in Git either.
3. [Configure harnesses](../how-to/configure-multiple-harnesses.md). The repository's harness list overrides your personal list, and each source can narrow it further. In the TUI, press `H` to edit the personal or project harness list.
4. Sync and check the result. Read any warnings about shared configuration files before you use new hooks or MCP servers. When sync changes a repository's Codex hooks or MCP servers, it warns that the change may need a trust review in Codex. `agent-marketplace` never grants that trust for you.

Check that each harness received the files it supports, and that another checkout can reproduce the shared setup without your personal configuration.

For resources on your own disk, you can use [live local directories](../how-to/use-a-local-registry.md), which have no lockfile. If symlinks don't work in your environment, choose [regular files instead of symlinks](../how-to/write-regular-files-instead-of-symlinks.md). To copy an existing harness layout once, without ongoing sync, use [one-shot conversion](../how-to/convert-a-harness-layout.md).

## 4. Check or update an existing setup

Checking for drift and installing a newer upstream revision are separate operations:

| Intent | Action | What changes |
|---|---|---|
| See what is selected and where it came from | `status` | Nothing; generated files stay as they are |
| Detect drift | `sync --check` | Nothing; it doesn't download missing cached content |
| Restore files from committed lockfiles | `sync --locked` | Cache and generated files; not configuration or lockfiles |
| Apply selection changes | `sync` | Generated files, and any writable lockfiles that are out of date or missing |
| Move to newer Git revisions and install them | `update --scope repo` | The chosen scope's Git lockfile and generated files |
| Move Git lockfiles forward separately | `lock --upgrade --scope repo` | Lockfiles only; run `sync` afterward to update the files |

A plain `sync` keeps a Git lockfile at its recorded commit, even when the branch has moved on. `update` covers every writable scope by default, so pass `--scope repo` to limit it to the repository. In the TUI, `u` moves Git lockfiles forward in every writable scope and syncs the files; `r` only reloads the current state from disk.

After the update, review the changes to the configuration and lockfile and run `sync --check`. When the check passes, commit the shared changes together.

Marketplace pins have extra rules. `update` moves a hosted resource to its newest revision, but it skips a request that sets an explicit `revision:` in the configuration, index-only pins, collection pins, and pins from the organization configuration, and it prints the reason for each. Online, the CLI can record explicit Marketplace requests in writable lockfiles, and it resolves them in memory when the organization lockfile is intentionally absent. It can't do that offline. Moving a hosted request to a different Marketplace, and snapshots for dynamic selectors, aren't supported. See [the command reference](../reference/cli.md#update).

## 5. Remove something without changing everyone else's setup

To remove a resource, or to opt out of an optional team selection:

1. Run `status` to find the source and scope that select it. The same name can exist in both the machine and repository scopes.
2. Follow [remove or override resources](../how-to/remove-or-override-resources.md), and pass `--scope` when needed. `remove` deletes a direct selection, or adds an exclusion when the resource was selected another way, such as through a tag.
3. If a hosted collection also selects the resource, removing the direct selection leaves the collection's selection in place. Remove the collection selector to stop selecting its members.
4. Run `status` and `sync --check` again. Confirm that you removed the selection you meant to and kept the others. Another scope or selection may still keep the resource installed.

Nothing can exclude a resource marked `required`, and a local override can't remove a machine-wide installation. If you delete generated files by hand, the next sync puts them back. To remove the whole setup, follow the [safe removal guide](../how-to/remove-agent-marketplace-safely.md), starting with its dry run.

## 6. Recover, work offline, or clean up

| Situation | Next step | What to watch for |
|---|---|---|
| Missing output or a failed drift check | [Troubleshoot sync drift](../how-to/troubleshoot-sync-drift.md) | Read the diagnostics before you rewrite configuration or lockfiles |
| A handwritten hook or MCP entry conflicts with generated output | [Repair shared configuration](../how-to/repair-owned-shared-configuration.md) | `doctor --repair --yes` can adopt exact matches; removing or replacing entries still asks for confirmation |
| A machine-wide install needs recovery | [Back up and restore](../how-to/back-up-and-restore-machine-resources.md) | These backups cover machine output, not repository history |
| No network connection | `sync --offline` in the [CLI reference](../reference/cli.md#sync) | Needs trusted cached content, and can't check for new revocations |
| Downloading from a private Git registry fails | [Configure private Git registries](../how-to/configure-private-git-registries.md) | Use the supported Git authentication, not credentials in committed URLs |
| The cache takes too much space | [Clean the cache safely](../how-to/clean-the-cache-safely.md) | Preview with `gc --dry-run`; the shared cache may hold resources that other repositories use, which this check doesn't cover |
| Review or change data collection | [Configure telemetry](../how-to/configure-telemetry.md) | Organization policy may limit your choices; inspect diagnostic payloads before you share them |
| Stop using `agent-marketplace` | [Remove it safely](../how-to/remove-agent-marketplace-safely.md) | Removing the managed files and removing the program are separate steps |

## Coverage and current limits

For command flags and less common options, see the [CLI reference](../reference/cli.md) and the [TUI reference](../reference/tui.md). To set up shell completion, see [completion](../reference/cli.md#completion).

These limits decide which parts of the journeys are available:

- In the browser, you can inspect resources and copy install commands. Run those commands in your terminal to install the resources for your harness.
- TUI Marketplace discovery shows at most 50 results per Marketplace and query, without pagination. Narrow the query, or use `agent-marketplace search` for more.
- `agent-marketplace` doesn't install prerequisites. Collections and tags group selections, but they don't resolve dependencies between resources.
- Publishing resources, running a Marketplace, and setting organization policy are outside these journeys.
