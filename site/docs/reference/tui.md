# Terminal UI reference

## Launch

- `agent-marketplace` starts the TUI when stdin is a terminal and `AGENT_MARKETPLACE_NO_TUI` is not set.
- `agent-marketplace tui` always launches it.

## Navigation

| Action | Key |
|---|---|
| Move up or down | `k` / `j` or arrow keys |
| Switch tab: Scope, Registry, Status | `1` / `2` / `3` |
| Fuzzy search | `/` |
| Toggle selected resource | `space` |
| View a resource's files | `enter` |
| Move in file browser: up / down | `j` / `k` |
| Fold / expand a directory | `h` / `l` or left / right |
| Return to resource list | `esc` or `q` |
| Sync pending changes | `s` |
| Refresh registry caches | `u` |
| Change target harnesses | `H` |
| Open the active config in `$EDITOR` | `E` |
| View errors and warnings | `e` |
| Show every binding | `?` |
| Quit | `q` |

## Modals

- `space` on a resource opens the scope toggle modal with keys `g`, `p`, `l`, `e`, and `esc`.
- `H` opens the harness selection modal. Toggle with `space` and confirm with `enter`.

## Minimum terminal size

The TUI requires at least 50 columns by 12 rows. Below that it shows a resize-required state and accepts only `q` and `Ctrl+C`.

## Mouse

Mouse tracking is not enabled. Drag selection remains terminal-owned so rendered text can be copied.

## Exit

- `q` follows the interactive quit flow and prompts to sync any pending changes.
- `Ctrl+C` and `SIGTERM` are hard exits. Filesystem work already completed is not rolled back.

## Related documentation

- [Quick start](../quick-start.md)
