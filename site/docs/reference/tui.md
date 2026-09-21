# Terminal UI reference

## Launch

- `agent-marketplace` starts the TUI when stdin is a terminal and `AGENT_MARKETPLACE_NO_TUI` is not set.
- `agent-marketplace tui` always launches it.

On first launch, choose target harnesses and save with `enter`. This creates editable v2 user configuration. Existing configuration is not automatically migrated to v4. V4 sources can be browsed and synchronized, but resource toggles cannot edit a v4 destination; edit its `sources` and `resources` maps directly. See [configuration compatibility](../explanation/user-journeys.md#before-following-a-journey).

## Browse, inspect, select, sync

1. Open **Registry** (`2`) to browse sources, or **Scope** (`1`) to see where resources are selected. Use `/` to search and `esc` to clear it.
2. Press `enter` on a resource to inspect it. Local resources open a file browser. A discovery-only Marketplace row shows metadata without downloading the artifact; an installed Marketplace resource can show files from its trusted cached bundle. If those bytes are missing, sync can restore them.
3. Press `space` to choose a selection scope. Review the modal's current state before toggling user, project, or local selection. Required resources cannot be excluded.
4. For a new Marketplace resource, review the verified revision preview and confirm the explicit installation scope. Search results alone do not authorize installation; blocked and revoked rows remain inspectable but cannot be installed.
5. Press `s` to synchronize pending changes, then check **Status** (`3`) and diagnostics (`e`). Resolve errors before relying on generated output.

Selection edits are written to configuration when accepted, before a later sync. “Pending” does not mean an unsaved draft: quitting without syncing does **not** undo configuration edits. Marketplace installation has its own verified preparation and confirmation flow.

## Hosted Marketplace discovery

Configured resources appear first; remote results arrive separately in **Available**, labeled with their Marketplace and registry. Search filters loaded rows immediately and starts remote search after about 250 milliseconds without input.

Each endpoint contributes at most 50 results per query, with no pagination. Narrow the query or use [`agent-marketplace search`](./cli.md#search) for more results. Discovery cache entries are fresh for five minutes and may be shown as stale fallback for up to 24 hours. An endpoint error does not by itself mean local files are out of sync; review discovery and sync diagnostics separately.

## Reload versus update

- `r` reloads configuration and local state from disk. Present valid locks keep discovery on their exact snapshots.
- `s` reconciles dependencies and generated output with the current selection. It does not advance a healthy Git lock merely because its branch moved.
- `u` deliberately upgrades Git locks in all writable scopes and synchronizes output. Despite the short “update caches” help label, this can change lockfiles and installed resources. Use CLI `update --scope repo` when you need a scope-limited update.

A missing layer lock can allow mutable browsing with a warning; an invalid existing lock does not silently fall back to mutable content. A failed authoritative state load allows inspection but blocks selection changes until resolved.

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

- `space` on a resource opens the scope modal: `g` toggles user/global selection, `p` project selection, `l` local selection, and `e` a local exclusion. `esc` cancels the modal. A local exclusion does not suppress a machine-wide or required resource.
- `H` opens the harness selection modal. Choose user/global with `g` or project with `p`, toggle harnesses with `space`, and save with `enter`. A project harness list takes precedence over the user list; there is no local-scope harness list.
- `E` opens the config picker: `g` for user/global, `p` for project, or `l` for local. These letters are modal actions; in the resource list, `g` jumps to the top and `e` opens diagnostics.

## Minimum terminal size

The TUI requires at least 50 columns by 12 rows. Below that it shows a resize-required state and accepts only `q` and `Ctrl+C`.

## Mouse

Mouse tracking is not enabled. Drag selection remains terminal-owned so rendered text can be copied.

## Exit

- `q` prompts when changes need synchronization: `y` syncs and quits, `n` quits without syncing, and `esc` cancels quitting. Existing config writes are retained in every case.
- `Ctrl+C` and `SIGTERM` are hard exits. Filesystem work already completed is not rolled back.

## Related documentation

- [Quick start](../quick-start.md)
- [User journeys](../explanation/user-journeys.md)
- [Browse the hosted Marketplace](../how-to/browse-marketplace-resources.md)
