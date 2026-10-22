## Why

Follow-up to #905 (#844). Its detail hint still required `!r.IsAvailable`, so an available row, and an installable Marketplace discovery row (also marked available), showed only `enter = full view`. Space on such a row in the list opens the toggle modal, so the hint hid a supported action. Gemini and CodeRabbit both flagged this on #905.

## What Changes

- The detail hint follows `rowToggleDenial` alone. Available and installable discovery rows now offer `space = toggle · enter = full view`; uninstallable discovery rows still do not.
- `tui-feedback` "Toggle hints follow row eligibility" names available and installable discovery rows explicitly.

## Capabilities

### New Capabilities

### Modified Capabilities
- `tui-feedback`: toggle hints include available rows.

## Impact

`internal/tui` detail hint only. `renderDetail` draws the list-focus right pane, where space toggles every eligible row.
