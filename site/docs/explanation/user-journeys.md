# User journeys

This page describes the lifecycle a resource goes through for the people who use it: how it is found, installed, kept up to date, and removed. It explains what each stage decides rather than giving commands to copy. Each stage links to the guide that walks through it. Publishing resources and running a Marketplace are outside this lifecycle and have their own guides.

```text
Find → Inspect → Choose scope and harnesses → Install → Use
                                                ↑        ↓
                                             Recover ← Check
                                                         ↓
                                                  Update or remove
```

## Configuration compatibility

The current configuration format is v4 (`apiVersion: agent-marketplace/v4`), which `init` and the TUI's first-run prompt create. On a v4 file, the supported edits include `install` and `remove` by name, `--from` for a Git or directory registry that is not configured yet, `install --tag` and `install --all`, Marketplace registration and installs, and TUI toggles for Git and directory sources plus the harness modal.

Every existing root, including organization and personal-local, must declare exact v4. Included fragments may omit the version or declare exact v4. Versionless roots fail with `v4.api-version-required`; explicit unsupported or invalid versions fail with `v4.api-version`, naming the file and field. There is no shipped `migrate` command, startup prompt, TUI migration modal, or legacy fallback. [Convert writable roots with an older released migrator before upgrading](../how-to/migrate-configuration.md); administrators convert organization configuration externally. Conversion can refuse includes, unrepresentable legacy input, or missing verified Marketplace evidence, and changing only the version string is insufficient. Some Marketplace cases are not supported: acquiring content offline when the organization lockfile is absent, rebinding hosted pins, and recording snapshots for dynamic selectors. When the CLI cannot verify a pin, it refuses and changes nothing.

## Where a resource lives

Before anything is installed, the [scope](../glossary.md#scope) decides which configuration file records the selection and which folders receive the output. A resource can belong to the machine (every project sees it, from the organization or user file), to the repository (shared with the team through the committed file), or to one checkout (the personal-local file, which writes into the repository's harness folders without a separate installation folder). Installing a resource defaults to `repo`; registering a Marketplace defaults to `user`. [Scopes, selection, and precedence](./scopes-selection-and-precedence.md) explains the model.

## Finding and choosing a resource

Resources come from three kinds of place: a Git registry address, a hosted Marketplace, or a source already in the configuration. `list` shows configured Git and directory sources, `search` queries hosted Marketplaces, and the TUI and the Marketplace browser offer the same discovery visually. Inspection before install matters: the resource kind, its declared prerequisites, its source, and its license all decide whether it belongs in your setup, and an automated scan cannot prove a resource is safe.

Two selection details are worth knowing early. A bare name can match more than one resource. Installation accepts a kind or a full [locator](../glossary.md#locator) to disambiguate; `remove` uses `--kind` and refuses ambiguous names. And `agent-marketplace` reports a resource's prerequisites but does not install them; the resource's own setup instructions apply. [Sources and resources](./sources-and-resources.md) explains how groups select resources once a source exists.

## Installing and checking

Installing records a selection in one configuration file and materializes the resource into each target harness. The [Git tutorial](../tutorials/install-your-first-skill.md) and the [Marketplace tutorial](../tutorials/install-your-first-marketplace-skill.md) walk through the two common installs. A Marketplace install command copied from the browser pins the revision you were viewing and works without registering the endpoint first; security findings are shown for consent before install, while capabilities are shown without needing consent.

After install, `status` shows what is selected where, and `sync --check` reports drift without changing anything. When a teammate has committed the repository configuration and its lockfile, `sync --locked` reproduces their exact setup in a fresh checkout or on CI; everyone reading that committed lockfile needs `agent-marketplace` v4.0.9 or newer. [Reproducibility and lockfiles](./reproducibility-and-lockfiles.md) explains what the lock records.

## Growing a setup

A setup can add more resource kinds (commands, subagents, hooks, and MCP servers alongside skills, often selected together through [tags, collections, or bundles](../how-to/install-resource-bundles.md)), more [harnesses](../how-to/configure-multiple-harnesses.md), and broader sharing. To share with a team, [adopt the CLI in the repository](../how-to/adopt-agent-marketplace-in-a-repository.md): the committed configuration and lockfile go to the team while the personal-local file and the generated harness output stay out of Git, and [personal overrides](../how-to/use-personal-repository-overrides.md) cover checkout-only additions or exclusions. When sync changes a repository's Codex hooks or MCP entries, it warns that Codex may ask for a trust review; the [harnesses reference](../reference/harnesses-and-output-paths.md) covers what each harness writes.

## Updating and removing

Checking for drift and moving to a newer revision are separate operations. A plain `sync` keeps a Git lockfile at its recorded commit even when the branch has moved on; `update` resolves the refs again, and `lock --upgrade` refreshes locks without syncing. Marketplace pins have extra rules: `update` moves a hosted resource to its newest revision but skips explicit `revision:` requests, index-only pins, collection pins, and organization-managed pins, printing the reason for each.

Removal edits the selection, not your files by hand: `remove` drops a direct selection or adds an exclusion when another selector, such as a tag, would keep selecting the resource. For Git and directory resources, a selected collection keeps providing its members until you exclude the member or remove the collection. For hosted Marketplace pins, removing a direct selection clears only that direct selection; the pin and the resource stay while a collection selector still includes it. Removing the collection removes the members nothing else keeps. A resource marked `required` cannot be excluded, and a local override cannot remove a machine-wide installation. Deleting generated files by hand only lasts until the next sync. [Remove or override resources](../how-to/remove-or-override-resources.md) covers the cases, and [safe removal](../how-to/remove-agent-marketplace-safely.md) covers removing the whole setup.

## Recovery and offline work

The lifecycle's failure paths are part of the design. A failed drift check or missing output is a signal to diagnose before rewriting configuration or lockfiles; [troubleshoot sync drift](../how-to/troubleshoot-sync-drift.md) starts there. Handwritten entries that conflict with generated output are reconciled by [repair](../how-to/repair-owned-shared-configuration.md), which can adopt exact matches and asks before removing or replacing anything. Offline work uses cached content through `sync --offline`, which cannot check for new revocations published after the cache was filled. Machine-scope output has its own [backup and restore](../how-to/back-up-and-restore-machine-resources.md) path, an overgrown cache can be [cleaned safely](../how-to/clean-the-cache-safely.md), and private Git registries need [their own credentials](../how-to/configure-private-git-registries.md). [Shared configuration ownership](./shared-configuration-ownership.md) explains why these guarantees hold.

## Current limits

These limits decide which parts of the lifecycle are available:

- In the browser, you can inspect resources and copy install commands; installation happens in your terminal.
- TUI Marketplace discovery shows at most 50 results per Marketplace and query, without pagination.
- `agent-marketplace` does not install prerequisites; collections and tags group selections but do not resolve dependencies between resources.

For command flags and less common options, see the [CLI reference](../reference/cli.md) and the [TUI reference](../reference/tui.md).
