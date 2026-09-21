# Terminal UI reference

## Launch

- `agent-marketplace` starts the TUI when stdin is a terminal and `AGENT_MARKETPLACE_NO_TUI` is not set.
- `agent-marketplace tui` always launches it.

On first launch, choose your target harnesses and press `enter` to save an editable v2 user config. The TUI leaves existing configs at their current version. You can browse and sync v4 sources, but resource toggles cannot edit v4 files. Edit their `sources` and `resources` maps directly; see [configuration compatibility](../explanation/user-journeys.md#before-following-a-journey).

## Browse, inspect, select, sync

1. Open **Registry** (`2`) to browse sources, or **Scope** (`1`) to see where resources are selected. Use `/` to search and `esc` to clear it.
2. Press `enter` on a resource to inspect it. Local resources open a file browser. For a Marketplace resource you haven't installed, the TUI shows metadata without downloading the artifact. Installed Marketplace resources can show files from their trusted cached bundles. If the cached files are missing, sync can restore them.
3. Press `space` to choose a scope. Check the current selection before toggling it for user, project, or local scope. Required resources cannot be excluded.
4. For a new Marketplace resource, review the verified revision preview and confirm the installation scope. Installation requires this check even if the resource appears in search. You can inspect blocked and revoked rows but cannot install them.
5. Press `s` to sync pending changes, then check **Status** (`3`) and diagnostics (`e`). Fix any errors before relying on the generated output.

The TUI saves selection changes to config as soon as you accept them. They remain saved if you quit without syncing; "pending" means the output still needs a sync. Marketplace installation has a separate verification and confirmation step.

## Hosted Marketplace discovery

Configured resources appear first. Remote results arrive in **Available**, labeled with their Marketplace and registry. Search filters the rows already loaded as you type, then searches remote endpoints after about 250 milliseconds without input.

The TUI shows at most 50 results per endpoint and query, with no pagination. Narrow the query or use [`agent-marketplace search`](./cli.md#search) for more results. Cached results stay fresh for five minutes. If discovery fails, the TUI may show stale results for up to 24 hours. Check discovery and sync diagnostics separately: an endpoint error alone does not mean your local files are out of sync.

## Reload versus update

- `r` reloads config and local state from disk. When a valid lock exists, discovery uses its exact snapshot.
- `s` syncs dependencies and generated output with your selections. A healthy Git lock stays at its recorded revision even if its branch has moved.
- `u` upgrades Git locks in all writable scopes and syncs output. The help label says "update caches", but this action can also change lockfiles and installed resources. Use CLI `update --scope repo` to limit an update to the repository.

If a layer has no lock, the TUI may browse its current source content and show a warning. An invalid existing lock blocks that fallback. If the TUI cannot load the current local state, you can still inspect resources, but must fix the load error before changing selections.

## Navigation

| Action | Key |
|---|---|
| Move up or down | `k` / `j` or arrow keys |
| Switch tab: Scope, Registry, Status | `1` / `2` / `3` |
| Next / previous tab | `tab` / `shift+tab` |
| Fuzzy search | `/` |
| Toggle selected resource | `space` |
| View resource details or cached files | `enter` |
| Move in file browser: up / down | `k` / `j` |
| Fold / expand a directory | `h` / `l` or left / right |
| Return to resource list | `esc` or `q` |
| Sync pending changes | `s` |
| Reload from disk | `r` |
| Upgrade Git locks and synchronize | `u` |
| Change target harnesses | `H` |
| Choose a config to open in `$EDITOR` | `E` |
| View errors and warnings | `e` |
| Show every binding | `?` |
| Quit | `q` |

## Modals

- `space` opens the resource's scope modal. Use `g` for user/global selection, `p` for project, `l` for local, or `e` for a local exclusion. Press `esc` to cancel. A local exclusion cannot suppress a machine-wide or required resource.
- `H` opens harness selection. Choose user/global with `g` or project with `p`, toggle harnesses with `space`, then save with `enter`. The project list overrides the user list. Local scope has no harness list.
- `E` opens the config picker: `g` for user/global, `p` for project, or `l` for local. These keys have different meanings outside the modals: in the resource list, `g` jumps to the top and `e` opens diagnostics.

## Minimum terminal size

The TUI requires at least 50 columns by 12 rows. Below that it shows a resize-required state and accepts only `q` and `Ctrl+C`.

## Mouse

Mouse tracking is not enabled. Drag selection remains terminal-owned so rendered text can be copied.

## Exit

- `q` prompts if changes need a sync. Choose `y` to sync and quit, `n` to quit without syncing, or `esc` to stay in the TUI. None of these choices undo saved config edits.
- `Ctrl+C` and `SIGTERM` are hard exits. Filesystem work already completed is not rolled back.

## Related documentation

- [Quick start](../quick-start.md)
- [User journeys](../explanation/user-journeys.md)
- [Browse the hosted Marketplace](../how-to/browse-marketplace-resources.md)
