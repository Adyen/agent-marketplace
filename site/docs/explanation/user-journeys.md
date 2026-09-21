# User journeys

Use this map to follow a resource from discovery to everyday use, updates, and removal. It covers people consuming resources through the CLI, terminal UI, and hosted Marketplace browser; publishing resources and operating a Marketplace are separate workflows.

```text
Find → Inspect → Choose scope and harnesses → Install → Use
                                                ↑        ↓
                                             Recover ← Check
                                                         ↓
                                                  Update or remove
```

## Before following a journey

[Install the CLI](../install.md) and select your target harnesses with `agent-marketplace init` or the TUI's first-run prompt. Browsing with `list` does not require harnesses; installing and synchronizing do.

::: warning Check your configuration version
V4 is the canonical configuration schema, but resource-selection editors are not yet v4-aware. `init` creates editable v2 user configuration. The CLI's `install` and `remove` operations, endpoint registration, and TUI resource toggles cannot edit a v4 destination file: they fail with `v4.writer-opt-in`.

For v2/v3, use the command and TUI editing flows below. For v4 Git and directory sources, edit `sources` and `resources` directly, then `sync`. V4 hosted requests additionally need matching verified layer-lock evidence; adding a source and digest by hand is not a complete installation flow. See [maintaining and migrating configuration](../how-to/migrate-configuration.md) before opting into v4.
:::

Choose where a resource belongs before installing it:

| Intent | CLI scope | Configuration | Output |
|---|---|---|---|
| Use it across my projects | `--scope user` | `~/.agents/config.yaml` | Machine harness roots |
| Share it with this repository's team | `--scope repo` | `.agents/config.yaml` | Repository harness roots |
| Use it only in my checkout | `--scope local` | `.agents/config.local.yaml` | Repository harness roots; private selection |

Resource installation defaults to `repo`; registering a Marketplace endpoint defaults to `user`. Local scope is not a separate harness installation directory. It changes the repository's effective selection. See [scope and precedence](./scopes-selection-and-precedence.md).

## 1. Find, inspect, and install a resource

**Start:** You know a Git registry URL, have a Marketplace link, or want to browse configured sources.

1. **Find it.** Use `list` for Git/directory registries, `list --from <git-url>` for an unconfigured Git registry, or `search <query>` for hosted Marketplaces. The [TUI](../reference/tui.md) brings configured sources together. The [Marketplace browser guide](../how-to/browse-marketplace-resources.md) covers search and filters in the web interface.
2. **Inspect it.** Open resource files with `enter` in the TUI, or review the browser's Content, Security, Revisions, and Registry tabs. Check the resource kind, prerequisites, source, and license. An automated scan is not a safety endorsement.
3. **Choose scope and targets.** Use the table above and check `status` for the effective target harness set. A resource can be selected without being supported by every target; review diagnostics and the [harness reference](../reference/harnesses-and-output-paths.md).
4. **Install.** Follow the [Git tutorial](../tutorials/install-your-first-skill.md) or [Marketplace tutorial](../tutorials/install-your-first-marketplace-skill.md). A browser's copied command pins the displayed revision and does not require prior endpoint registration. For Marketplace warnings or active capabilities, review the verified CLI preview before accepting. Blocked or revoked revisions cannot be accepted through that prompt.
5. **Verify and use.** Run `status` and `sync --check`, then open or reload the coding harness in the intended project. Agent Marketplace installs resources; the harness provides the interface for using them.

**Done when:** The intended resource, scope, and harness output appear in `status`, and the drift check exits `0`.

**If blocked:** Disambiguate names with `--kind` or a source-specific locator; use `--as` for a Marketplace local-name conflict. Fix prerequisites in the resource's instructions: they are advisory, not automatically installed dependencies. See [hosted installation troubleshooting](../how-to/install-from-a-marketplace.md#troubleshooting).

## 2. Join a repository that already has resources

**Start:** A teammate committed the repository configuration and its generated lock.

1. Clone the repository and run commands inside it.
2. Run `agent-marketplace sync --locked`. This fetches the exact locked Git snapshots and generates harness output without rewriting the config or locks.
3. Run `agent-marketplace status` to see targets and source layers, then `agent-marketplace sync --check` to confirm the prepared checkout has no drift.
4. Open your coding harness. If you need a personal addition or exclusion, use [personal repository overrides](../how-to/use-personal-repository-overrides.md), not a team-config edit.

**Done when:** Your checkout has the team's selected resources at the locked revisions. Your user and organization layers can still add machine-wide resources; repository locks do not erase those layers.

**If blocked:** A missing or stale committed lock needs reconciliation by someone updating the repository. A cold CI runner must run `sync --locked` before `sync --check`: the check does not download snapshots or create output. See [reproducibility](../how-to/keep-resources-reproducible.md) and [drift recovery](../how-to/troubleshoot-sync-drift.md).

## 3. Build a personal or shared setup

**Start:** One resource works and you want a reusable set, another harness, or a checkout-only change.

1. Select additional kinds—commands, subagents, hooks, or MCP servers—as well as skills. Use [tags, collections, or registry bundles](../how-to/install-resource-bundles.md) when you want a group.
2. Choose the owning scope. Commit repository configuration and its lock for teammates; keep local configuration and its lock ignored. Do not commit generated output as the source of truth.
3. [Configure harnesses](../how-to/configure-multiple-harnesses.md). Repository targets take precedence over user targets; per-source restrictions can narrow the effective set. In the TUI, `H` selects the user or project harness list.
4. Synchronize and check the result. Review shared-configuration warnings before using newly installed hooks or MCP servers. Codex repository changes can require a separate trust review in Codex; Agent Marketplace does not grant that trust.

**Done when:** Each intended harness has supported output, and another checkout can reproduce the shared part without your personal config.

**Variants:** [Live local directories](../how-to/use-a-local-registry.md) are useful for local resources but are not Git-locked. Use [file materialization](../how-to/write-regular-files-instead-of-symlinks.md) when symlinks do not suit the environment. [One-shot conversion](../how-to/convert-a-harness-layout.md) copies an existing harness layout without enrolling it in ongoing synchronization.

## 4. Check or update an existing setup

**Start:** Resources are installed and you want to distinguish drift from a new upstream release.

| Intent | Action | What changes |
|---|---|---|
| See what is selected and where it came from | `status` | No output reconciliation |
| Detect drift | `sync --check` | Read-only; does not populate a cold cache |
| Restore output using committed locks | `sync --locked` | Cache and generated output; not config or locks |
| Reconcile edited selection | `sync` | Output and, when needed, writable stale/missing locks |
| Advance Git revisions and install them | `update --scope repo` | Selected scope's Git locks and generated output |
| Advance Git locks separately | `lock --upgrade --scope repo` | Locks; run `sync` afterward to apply output |

An ordinary sync does not advance a healthy Git lock merely because its branch moved. `update` defaults to all writable scopes, so pass a scope when you intend a repository-only update. The TUI's `u` action upgrades Git locks across writable scopes and synchronizes; it is not just a read-only reload (`r`).

Review the resulting config/lock diff, run `sync --check`, and commit shared changes together. For hosted resources, legacy mirror pins can be updated; index-only and organization-required pins have restrictions. General hosted v4 acquisition/update and dynamic-selector snapshots are not implemented. See [the command reference](../reference/cli.md#update).

**Done when:** The intended revisions are installed, the drift check passes, and shared config/lock changes are committed.

## 5. Remove something without changing everyone else's setup

**Start:** You no longer want a resource, or want to opt out of an optional team selection.

1. Check `status` for the owning source and scope. The same name can exist in both machine and repository scopes.
2. Follow [remove or override resources](../how-to/remove-or-override-resources.md), passing an explicit scope when needed. Removal deletes a direct selection or records an exclusion for an indirectly selected resource.
3. If it is a hosted collection member, removing a direct selection does not remove the collection's ownership. Remove the collection selector if you intend to stop selecting its members.
4. Check `status` and `sync --check` again. Verify the targeted occurrence disappeared; another scope or selection may legitimately keep it installed.

**Done when:** The unwanted occurrence is no longer selected and the other intended occurrences remain.

**If blocked:** Required resources cannot be excluded. A local override cannot suppress machine-wide installation. Deleting generated files by hand is temporary: sync recreates selected resources. For complete teardown, use the [safe removal guide](../how-to/remove-agent-marketplace-safely.md), starting with its dry run.

## 6. Recover, work offline, or clean up

| Situation | Next step | Boundary to remember |
|---|---|---|
| Missing output or a failed drift check | [Troubleshoot sync drift](../how-to/troubleshoot-sync-drift.md) | Read diagnostics before rewriting config or locks |
| A handwritten hook/MCP entry conflicts with generated output | [Repair shared configuration](../how-to/repair-owned-shared-configuration.md) | `doctor --repair --yes` can adopt exact matches; removal/replacement still requires confirmation |
| A machine-wide install needs recovery | [Back up and restore](../how-to/back-up-and-restore-machine-resources.md) | These backups cover machine output, not repository history |
| No network connection | `sync --offline` in the [CLI reference](../reference/cli.md#sync) | Requires trusted cached content; cannot learn new revocations |
| Private Git acquisition fails | [Configure private Git registries](../how-to/configure-private-git-registries.md) | Use the supported Git authentication path, not credentials in committed URLs |
| Cache consumes too much space | [Clean the cache safely](../how-to/clean-the-cache-safely.md) | Preview with `gc --dry-run`; cache is shared, but reachability covers only the current context |
| Review or change data collection | [Configure telemetry](../how-to/configure-telemetry.md) | Organization policy may constrain preferences; inspect diagnostic payloads before sharing |
| Stop using Agent Marketplace | [Remove it safely](../how-to/remove-agent-marketplace-safely.md) | Teardown and removing the binary are separate steps |

## Coverage and current limits

These journeys connect the consumer CLI, TUI, and browser entry points to existing task guides. For flags and less common variants, use the [CLI reference](../reference/cli.md) and [TUI reference](../reference/tui.md). Shell completion is covered under [completion](../reference/cli.md#completion).

This is a map of supported workflows, not a promise that every resource works in every harness. Keep the following limits in mind:

- The browser inspects and copies install commands; it does not install into your local harness itself.
- TUI Marketplace discovery shows at most 50 results per endpoint/query, without pagination. Narrow the query or use CLI search for more.
- Configuration v4 reads and explicit migration do not imply v4 resource-editor support or general hosted acquisition support.
- Resource prerequisites are not a dependency installer. Collections and tags group selections; they do not supply a dependency-resolution mechanism.
- Publishing resources, administering a Marketplace, and configuring organization policy are outside these consumer journeys.
