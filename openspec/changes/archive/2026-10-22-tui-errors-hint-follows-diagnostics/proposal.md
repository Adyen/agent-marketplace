## Why

The resource-list footer offered `e errors` whenever drift was out of sync. With plain pending drift, for example after a toggle or a Marketplace install, the errors view has nothing to show and `e` does nothing, so the hint advertised a dead action (#824). The palette already hid the action in that state.

## What Changes

- The footer offers `e errors` exactly while there is an error, warning or discovery error, using the same predicate as the palette action and the `e` key. Plain drift loses the hint, and a warnings-only state, which previously lacked it, now shows it. `r reload` keeps its existing conditions.
- The `hostile-metadata` journey no longer uses the 80-column `space toggle` hint as a layout marker: with its malformed-skill warnings the footer now spends that room on `e errors`.
- `tui-feedback` states the rule in "Discoverable footer actions".

## Capabilities

### New Capabilities

### Modified Capabilities
- `tui-feedback`: the errors hint follows the presence of diagnostics.

## Impact

`internal/tui` footer hints; the palette and the `e` handler share the new `hasDiagnostics` predicate without behavior change.
