# Terminal UI reference

This page lists the keys, tabs, dialogs, and states of the interactive terminal UI (the [TUI](../glossary.md#tui)). Use it to look up a key or to understand what the TUI saves and when. For a guided first run, see the [quick start](../quick-start.md).

## Launch

- `agent-marketplace` starts the TUI when standard input is a terminal and `AGENT_MARKETPLACE_NO_TUI` is not set.
- `agent-marketplace tui` always starts it.

If you have no user config yet, the TUI first asks you to choose target harnesses. Harnesses it detects on your machine are pre-selected. Toggle harnesses with `space` and press `enter` to save a v4 user config (`~/.agents/config.yaml`), or `esc` to cancel. An existing file is left unchanged.

If your user config still uses a legacy v2 or v3 schema, the TUI opens a **Configuration migration required** dialog. Press `enter` or `m` to migrate it to v4, or `l` or `esc` to leave it unchanged. v2 and v3 files stay readable if you decline. Migration runs as one transaction and leaves unsupported or lossy input unchanged. The dialog only covers the user config; migrate repository and personal-local configs with [`agent-marketplace migrate`](./cli.md#migrate).

Resource toggles can edit v4 configs for configured Git and directory sources, and the harness dialog (`H`) can edit the target harness list. Some other edits, such as some hosted Marketplace changes, may still be refused for a v4 file; a refusal leaves the file unchanged. In that case edit the `sources` and `resources` maps directly. See [configuration compatibility](../explanation/user-journeys.md#before-following-a-journey).

## Browse, inspect, select, sync

1. Open **Registry** (`2`) to browse sources, or **Scope** (`1`) to see where resources are selected. Press `/` to search and `esc` to clear the search.
2. Press `enter` on a resource to inspect it. Local resources open a file browser. For a Marketplace resource you have not installed, the TUI shows its metadata without downloading anything. Installed Marketplace resources show files from their verified cached bundle. If those cached files are missing, a sync can restore them.
3. Press `space` to open the scope dialog. Check the current selection before you toggle it for user, project, or local scope. Required resources cannot be excluded.
4. Selecting a new Marketplace resource verifies its exact revision before installing it. If the revision has security findings, the TUI shows the verified preview and asks you to confirm. Without findings, it installs the prepared revision with no extra prompt. Capability metadata alone never asks for consent. You can inspect blocked and revoked rows, but you cannot install them.
5. Press `s` to sync pending changes, then check **Status** (`3`) and the diagnostics view (`e`). Fix any errors before you rely on the generated files.

The TUI saves a selection change to config as soon as you accept it. The change stays saved even if you quit without syncing. "Pending" means the harness folders still need a sync to match your config.

## Hosted Marketplace discovery

Configured resources appear first. Results from each Marketplace then arrive in the **Available** group, labeled with their Marketplace and registry. As you type a search, the TUI filters the rows it already has: resource names match fuzzily, and other details such as tags, registry, and description match when each query word starts one of their words. Name matches sort first within each group and the best match is selected. After `enter`, the query and its match count stay above the list; `esc` clears it, and `/` resumes editing it. About 250 milliseconds after you stop typing, it also searches the remote endpoints. Clearing the search brings back the default results.

The TUI shows at most 50 results per endpoint and query, with no pagination. Narrow the query, or use [`agent-marketplace search`](./cli.md#search) for more results. Cached results stay fresh for five minutes. If discovery fails, the TUI can keep showing stale results for up to 24 hours. Discovery errors and sync state are separate: an endpoint error alone does not mean your local files are out of sync.

## Reload versus update

- `r` reloads config and local state from disk. When a valid lock exists, discovery uses the exact snapshot it records.
- `s` syncs dependencies and generated output with your selections. A healthy Git lock stays at its recorded commit even if its branch has moved.
- `u` upgrades Git locks in all writable scopes (user, project, and local) and then syncs. The help label says "update caches", but this key can also change lockfiles and installed resources. To update only the repository, run `agent-marketplace update --scope repo` instead.

If a config layer has no lock, the TUI can browse its source's current content and shows a warning. It does not create the lock. An invalid lock that exists blocks that fallback.

A configuration load error blocks selection changes until you fix the file with `E`. Catalog, resolution, and materialization failures keep the loaded rows visible. Rows that can normally be toggled still open the scope dialog, including selections shown as unavailable, and drift stays marked as failed.

The status bar reports sync drift, Marketplace endpoint health, the active operation, and error or warning counts when space permits. It shows `running` until an operation reports a phase it has entered. Progress alone never reports success; the operation and its on-disk reload must finish. Up to three short result notices appear as toasts and expire after a few seconds. The footer always shows the command palette (`:`), help (`?`), and quit (`q`) keys; other key hints drop one at a time as the terminal narrows. While errors are present, the error, reload, and quit keys come first.

## Keys

| Action | Key |
|---|---|
| Move up or down | `k` / `j` or arrow keys |
| Half page down / up | `ctrl+d` / `ctrl+u` |
| Page down / up | `f`, `ctrl+f`, or `pgdown` / `b`, `ctrl+b`, or `pgup` |
| Jump to top / bottom | `g` or `home` / `G` or `end` |
| Switch tab: Scope, Registry, Status | `1` / `2` / `3` |
| Next / previous tab | `tab` / `shift+tab` |
| Fuzzy search | `/` |
| Apply / clear search | `enter` / `esc` |
| Open the scope dialog, or fold a section header | `space` |
| View resource details or cached files | `enter` |
| Collapse / expand a section | `h` / `l` or left / right |
| Toggle the current fold / collapse all / expand all | `za` / `zM` / `zR` |
| Open the command palette | `ctrl+k` or `:` |
| Sync pending changes | `s` |
| Reload from disk | `r` |
| Upgrade Git locks and sync | `u` |
| Change target harnesses | `H` |
| Choose a config to open in `$EDITOR` | `E` |
| View errors and warnings | `e` |
| Show every binding | `?` |
| Quit | `q` |

In the file browser that `enter` opens, `j` / `k` and the arrow keys move through entries, `h` / left folds a directory, `l` / right expands it, and `enter` toggles a directory. `esc` or `q` returns to the resource list.

The command palette (`ctrl+k` or `:`) lists the resource-list actions — search, clear search (while a filter is applied), details, toggle, reload, sync, update, harnesses, config editor, help, diagnostics, tab switches, fold commands, and quit — as a fuzzy-filtered overlay, with each action's key shown beside it. It opens only over the resource list at a supported size: not while a search, dialog, or overlay is active, not while a `z` fold prefix is pending, and not below the minimum terminal size. Type to filter the action labels (`q` and other letters are filter text, not list keys), move with `up` / `down`, run the highlighted action with `enter`, and close the palette with `esc`; `ctrl+c` still exits immediately. Actions that are not currently allowed are hidden, eligibility is checked again when you confirm, and the same guards and confirmations apply as for the key itself.

## Dialogs

- `space` opens the resource's scope dialog. Press `g` for user (global) selection, `p` for project, `l` for local, or `e` for a local exclusion. Press `esc` to cancel. A local exclusion cannot hide a machine-wide or required resource. For an optional organization Marketplace resource, `e` instead edits an exact exclusion in your v4 user config; required resources stay protected.
- `H` opens harness selection. Choose user (global) with `g` or project with `p`, toggle harnesses with `space`, then save with `enter` or cancel with `esc`. The project list overrides the user list. Local scope has no harness list.
- `E` opens the config picker: `g` for user (global), `p` for project, or `l` for local; `esc` cancels. The TUI opens the file in the editor named by `$EDITOR`, or `vi` if it is not set, and reloads when the editor exits. While the editor runs, the terminal shows which file is open instead of the shell.

Inside a dialog, these letters have dialog meanings. In the resource list, `g` jumps to the top and `e` opens diagnostics.

## Minimum terminal size

The TUI needs at least 50 columns by 12 rows. In a smaller window it shows only a resize message and accepts only `q` and `ctrl+c`. Making the window large enough again restores where you were.

## Mouse

The TUI does not capture the mouse. You can select rendered text with the mouse and copy it as usual in your terminal.

## Logging while the TUI runs

While the TUI is on screen, logs still go to the log file but are never printed to the terminal, even with `--debug`. See the [logging reference](./logging.md).

## Exit

- `q` asks what to do if changes still need a sync. Press `y` to sync and quit, `n` to quit without syncing, or `esc` to stay. None of these choices undoes config edits that were already saved.
- `ctrl+c`, `SIGINT`, and `SIGTERM` exit immediately. Filesystem work that already finished is not rolled back; the next launch shows the resulting state.

## Related documentation

- [Quick start](../quick-start.md)
- [User journeys](../explanation/user-journeys.md)
- [Browse the hosted Marketplace](../how-to/browse-marketplace-resources.md)
