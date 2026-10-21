# Terminal UI reference

Keys, dialogs, and states of the [TUI](../glossary.md#tui), and what it saves when. For a guided first run, see the [quick start](../quick-start.md).

## Launch

- `agent-marketplace` starts the TUI when standard input is a terminal and `AGENT_MARKETPLACE_NO_TUI` is not set.
- `agent-marketplace tui` always starts it.

If you have no user config yet, the TUI asks you to choose target harnesses once the config files validate. Harnesses it detects are pre-selected. Toggle them with `space`, then press `enter` to save a v4 user config (`~/.agents/config.yaml`) or `esc` to cancel. An existing file is left unchanged.

Every existing config root, including organization and personal-local, must declare exactly `apiVersion: agent-marketplace/v4`. Included fragments may omit the field or declare exact v4. Otherwise the TUI shows a `v4.api-version-required` or `v4.api-version` diagnostic naming the file and blocks actions that need the configuration. It never offers to convert the file and has no legacy reader. [Convert writable roots with an older released migrator before upgrading](../how-to/migrate-configuration.md); organization configuration is converted by its administrator.

Resource toggles, Marketplace installs, and the harness dialog (`H`) edit v4 config and then reload it from disk. A finished form does not by itself mean the write succeeded; the reload does. Cancel, sync-and-quit, and quit stay available. A refused operation starts no new work: it downloads nothing and writes no config, lock, or harness files of its own. It may still write logs, and recovery may first finish an interrupted earlier write, which changes the files that write recorded. Hosted runtime options such as dynamic selectors and hosted rebind are refused; see [current limits](../explanation/user-journeys.md#before-following-a-journey).

## Resource list

The list groups resources by status (**Required**, **Enabled**, **Pending sync**, **Stale**, **Broken**, **Available**, **Malformed**) and hides empty groups. Each row shows a scope letter (`G` user, `P` project, `L` local, their combinations, `R` required, `P-` a project selection overridden by a local exclusion, blank for available or malformed rows), the resource kind, and `registry/name` (the source's repository name, or `(local)`). `/` searches and `esc` clears; search also matches kind and source labels in every grouping.

`v` opens **View settings** to group by **Source** or **Kind** instead, optionally with a second grouping inside each section. `tab` or `↑`/`↓` move between the two fields, `←`/`→` (or `h`/`l`) change a field, `enter` accepts, and `esc` restores the previous grouping. Folds and the selected resource are kept. Outside the status grouping, rows that need attention appear once, in a pinned **Needs attention** section at the top, and not in their own section. A confirmed grouping is saved to `$XDG_STATE_HOME/agent-marketplace/tui.json` (default `~/.local/state/agent-marketplace/tui.json`) and restored on the next launch. It never touches a config or lockfile. A missing file silently falls back to the status grouping; a malformed, unreadable, or unsupported file falls back with a footer warning and is left alone until you confirm a change.

## Inspection

`enter` inspects a resource:

- Local resources open a file browser.
- An installable Marketplace skill loads the verified inspection of its exact revision in the background, showing `SKILL.md` first and fetching supporting text files when you select them. Browsing installs nothing and changes no config or lock.
- Binary files show identity details. Unavailable, corrupt, or mismatched content shows an error.
- Other uninstalled Marketplace rows show metadata without downloading content.
- Installed resources show their verified cached files, including a synthesized `SKILL.md` when supporting files are cached apart from the definition. Sync restores missing cached files.

## Selection

`space` on a resource opens the scope dialog (user, project, or local; see [Dialogs](#dialogs)). Required resources cannot be excluded. On a section header, `space` opens a bulk toggle for every eligible row in the section.

A new Marketplace resource has its exact revision verified before it is installed. If the revision has security findings, the TUI shows the verified preview and asks for confirmation; without findings it installs with no prompt. Capability metadata alone never triggers a prompt. Blocked and revoked rows can be inspected but not installed.

A selection change is saved to config as soon as you accept it, and stays saved if you quit without syncing. "Pending" means the harness folders still need a sync to match the config.

## Sync state

`s` syncs pending changes into the harness folders. The **Pending sync** group and the diagnostics view (`e`) show that run's materialization errors and warnings. Each error shows the same next-step guidance the CLI prints on its `hint:` line when the error's code has any, falling back to the view's own recovery copy otherwise.

## Hosted Marketplace discovery

Configured resources appear first, then each Marketplace's results arrive in the **Available** group, labeled with Marketplace and registry. Typing a search filters the rows already loaded: names match fuzzily, and tags, registry, and description match when each query word starts one of their words. Name matches sort first in each group, and the best match is selected. After `enter`, the query and match count stay above the list; `esc` clears it and `/` edits it again. About 250 milliseconds after you stop typing, the TUI also queries the remote endpoints. Clearing the search restores the default results.

The TUI shows at most 50 results per endpoint and query, with no pagination; [`agent-marketplace search`](./cli.md#search) returns more. Cached results stay fresh for five minutes, and after a failed discovery the TUI can show stale results for up to 24 hours. An endpoint error alone does not mean your local files are out of sync.

## Reload versus update

- `r` reloads config and local state from disk. With a valid lock, discovery uses the exact snapshot it records.
- `s` syncs dependencies and generated output with your selections. A healthy Git lock stays at its recorded commit even if the branch has moved.
- `u` upgrades Git locks in all writable scopes (user, project, and local), then syncs. Its help label says "update caches", but it can also change lockfiles and installed resources. `agent-marketplace update --scope repo` does the repository-only equivalent. Before publishing, `u` applies the same selection check as `agent-marketplace update`. When a selection that resolved at the previously locked revision is renamed or removed upstream, the update fails with the same `was selected but not found` refusal and remediation line, and the locks, configuration, and harness outputs stay unchanged.

If a config layer has no lock, the TUI browses its source's current content and shows a warning; it does not create the lock. An existing invalid lock blocks that fallback.

If a valid lock names an exact Git snapshot that is not cached, for example after you pull an updated repository lock, the TUI warns you to press `s` or run `agent-marketplace sync --locked`. `r` does not fetch the snapshot. The affected selections and drift stay unverified until sync succeeds; the TUI never substitutes an older cache or the current branch. Sync fetches the locked commit without advancing a healthy lock. Corrupt snapshots and failed syncs remain errors, and CLI check and offline commands still fail while required snapshots are missing.

A Marketplace can republish a resource's archive without changing its content (an archive-evidence repair). If your lock still pins the old archive digest, `s` and `u` update the lock to the digest now served and re-download the verified artifact. The run still succeeds: the toast shows `Sync complete with N warning(s)` (or `Update complete with N warning(s)`), and `e` lists the `marketplace.archive-evidence-refreshed` warning. With nothing to repair, the run stays quiet and does not rewrite the lock. If the new artifact cannot be fetched, or the lock is in a read-only scope such as a deployed organization lock, the operation fails and leaves your files and lock unchanged. See [lockfiles](./lockfiles.md) for the fields involved and `sync --locked`/`sync --check` behavior.

A configuration load error blocks selection changes until the file is fixed (`E` opens it in an editor). Catalog, resolution, and materialization failures keep the loaded rows visible: toggleable rows, including ones shown as unavailable, still open the scope dialog, and drift stays marked as failed.

The status bar shows sync drift, Marketplace endpoint health, the running operation, and error and warning counts when there is room. It shows `running` until the operation reports its first phase. Progress never means success; the operation and its reload from disk must finish. Up to three short notices appear as toasts and expire after a few seconds. The footer always shows the command palette (`:`), help (`?`), and quit (`q`) keys; other hints drop out one at a time as the terminal narrows. While there are errors, the error, reload, and quit keys come first.

## Keys

| Action | Key |
|---|---|
| Move up or down | `k` / `j` or arrow keys |
| Half page down / up | `ctrl+d` / `ctrl+u` |
| Page down / up | `f`, `ctrl+f`, or `pgdown` / `b`, `ctrl+b`, or `pgup` |
| Jump to top / bottom | `g` or `home` / `G` or `end` |
| Fuzzy search | `/` |
| Apply / clear search | `enter` / `esc` |
| Open the resource's scope dialog, or the section bulk-toggle dialog on a section header | `space` |
| Open View settings (grouping) | `v` |
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

In the file browser, `j` / `k` and the arrow keys move, `h` / left folds a directory, `l` / right expands it, `enter` toggles it, and `esc` or `q` returns to the list.

The command palette (`ctrl+k` or `:`) is a fuzzy-filtered list of resource-list actions with their keys: search, clear search (while a filter is applied), details, toggle, reload, sync, update, harnesses, config editor, view settings, help, diagnostics, fold commands, and quit. It opens only over the resource list at a supported size, and not while a search, dialog, or overlay is open or a `z` fold prefix is pending. Typed characters filter the labels (`q` and other letters are filter text here). `up` / `down` move, `enter` runs the action, `esc` closes, and `ctrl+c` still exits. Disallowed actions are hidden, eligibility is checked again on confirm, and each action has the same guards and confirmations as its key.

## Dialogs

- **Scope** (`space` on a resource): `g` user (global), `p` project, `l` local, `e` local exclusion, `esc` cancel. A local exclusion cannot hide a machine-wide or required resource. For an optional organization Marketplace resource, `e` edits an exact exclusion in the v4 user config instead; required resources stay protected.
- **Section bulk toggle** (`space` on a section header): counts matching, eligible, and skipped rows (including folded and filter-matched descendants) and says why each was skipped. `g`, `p`, `l`, or `e` pick a scope, `+` or `-` a direction, `enter` confirms, and `esc` cancels at any stage. All eligible rows change in one transaction, and a failure leaves the layer untouched. Rows are checked again on confirm; a section whose members span several config layers asks you to pick a narrower one.
- **Harnesses** (`H`): `g` edits the user (global) list, `p` the project list, `space` toggles a harness, `enter` saves, `esc` cancels. The project list overrides the user list. Local scope has no harness list.
- **Config picker** (`E`): `g` user (global), `p` project, `l` local, `esc` cancel. The file opens in `$EDITOR` (or `vi`), and the TUI reloads when the editor exits. Meanwhile the terminal shows which file is open.

These letters mean something else outside dialogs: in the resource list, `g` jumps to the top and `e` opens diagnostics.

## Minimum terminal size

The TUI needs at least 50 columns by 12 rows. Below that it shows only a resize message and accepts only `q` and `ctrl+c`. Enlarging the window restores where you were.

## Mouse

The TUI enables cell-motion mouse reporting to receive scroll-wheel input. A vertical wheel event moves the resource-list or bundle-tree selection by one entry, or scrolls the file preview, help, errors, and Marketplace consent views by one line.

A left click on a resource-list row selects it; clicking the selected resource opens its file browser, like `enter`. Clicking a section header folds or unfolds it. In the file browser, clicking a file selects and previews it, and clicking a directory expands or collapses it. A click counts only if you press and release on the same cell, so a drag that starts in the list or tree does nothing. Modifier clicks do nothing, as do clicks on the header, search row, footer, an empty row, the right-hand pane, an overlay, a dialog, or the command palette, and clicks while you type a search.

Dragging with the left button in the details or file-preview pane, or across the diagnostics on the errors screen, highlights text and copies it on release. The errors-screen footer and blank padding are not selectable. Copying uses OSC52, which the terminal or multiplexer must support for text to reach the system clipboard. A click without a drag, or a drag that starts elsewhere, selects nothing. Scrolling, resizing, switching resource or file, or a change to the displayed diagnostics clears the highlight; `esc` dismisses it first. Other clicks, releases, and horizontal wheel events do nothing; Shift+vertical-wheel still scrolls vertically. During wheel bursts the list redraws at up to 60 frames per second, so fast scrolling always settles on the final selection. Because the TUI receives mouse events, native text selection may need a terminal-specific modifier (often Option or Shift).

## Logging while the TUI runs

While the TUI is on screen, logs go to the log file and are never printed to the terminal, even with `--debug`. See the [logging reference](./logging.md).

## Exit

- `q` asks what to do when changes still need a sync: `y` syncs and quits, `n` quits without syncing, `esc` stays. None of these undoes config edits already saved.
- `ctrl+c`, `SIGINT`, and `SIGTERM` exit immediately. Finished filesystem work is not rolled back; the next launch shows the result.

## Related documentation

- [Quick start](../quick-start.md)
- [User journeys](../explanation/user-journeys.md)
- [Browse the hosted Marketplace](../how-to/browse-marketplace-resources.md)
