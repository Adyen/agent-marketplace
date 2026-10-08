## Why

An applied fuzzy query forces every matching category open, so Enter and other fold controls cannot collapse it. Browsing a large result set needs working category folds without losing the folds saved before search.

## What Changes

- Allow existing fold controls to collapse and expand categories while a query is applied.
- Keep search folds separate from browsing folds, reset them when the query changes, and preserve them when results refresh for the same query.
- Restore browsing folds when the query clears.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `tui-navigation`: define category fold behavior within an applied query and across query changes and result refreshes.

## Impact

The TUI's in-memory fold state and keyboard regression tests change. Search ranking, configuration writes, installation behavior, and remote discovery APIs remain unchanged.
