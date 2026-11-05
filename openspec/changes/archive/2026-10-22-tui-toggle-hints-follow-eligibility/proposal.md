## Why

The detail pane ended with `space = toggle · enter = full view`, and the footer showed `space toggle`, for every enabled row, including required rows whose space only shows the "required items cannot be toggled" toast (#844). The hints advertised an action the row refuses.

## What Changes

- `rowToggleDenial` holds the row's own toggle guards (malformed, pending verification, uninstallable Marketplace, required, collection-only). `rowToggleEligibility` still applies the transient busy and invalid-config guards first.
- The detail hint and the footer's `space toggle` hint follow `rowToggleDenial` for the selected row; section headers keep the footer hint.
- `tui-feedback` adds "Toggle hints follow row eligibility".

## Capabilities

### New Capabilities

### Modified Capabilities
- `tui-feedback`: toggle hints follow the selected row's eligibility.

## Impact

`internal/tui` detail and footer hints. The toggle guards themselves are unchanged.
