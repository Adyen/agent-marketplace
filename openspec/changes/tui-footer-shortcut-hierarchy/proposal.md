## Why

In the resource list footer, `footerFloor()` (`:`, `?`, `q`) renders its shortcut keys in the accent color while `footerActionHints()` (`/`, `space`, `s`, `H`, `E`, `v`, `i`, `r`, `e`) renders both key and description in the muted color. This inverts visual hierarchy: navigation/meta keys stand out while primary actions look dimmed or unavailable.

## What Changes

- Render every active shortcut key in the resource list footer with the same emphasized key style paired with muted description text, instead of highlighting only `:`, `?`, and `q`.
- Preserve existing footer layout priority, truncation order, no-color readability, and minimum-width (50×12) floor guarantees (`:`, `?`, `q` and search-mode `enter`, `esc`, `ctrl+c`).
- Add or extend a real-terminal TUI QA journey verifying the rendered footer shortcut styling and visible primary hints.

## Capabilities

### Modified Capabilities

- `tui-feedback`: consistent shortcut key emphasis across all active footer hints.

## Impact

`internal/tui/footer.go`, `internal/tui/theme.go`, footer unit tests, and `tools/tui-qa/tests/`. No keybindings, priority ranks, or drop order change.

## Release note

All active keyboard shortcuts in the TUI footer now highlight their key caps consistently rather than emphasizing only `:`, `?`, and `q`.
