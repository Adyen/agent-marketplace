## Why

On the Source step of the install-from-source modal, only Tab and Shift+Tab
move between the URL, Ref, and Path fields. Users reach for `↑` and `↓` to
step between stacked fields, as they already do on the Review step and in
View settings, and those keys currently do nothing there.

## What Changes

- On the Source step, `↓` moves focus from URL to Ref to Path and `↑` moves
  it back, without wrapping. Tab and Shift+Tab keep wrapping. `k` and `j`
  stay text input.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-interactions`: adds arrow-key field focus to the install-from-source
  Source step.

## Impact

- `internal/tui`: install-from-source Source-step key handling, footer hint.
- `tools/tui-qa`: install-from-source journey.
- Docs: `site/docs/reference/tui.md`, `docs/tui-product-contract.md`.
