## Why

Real-terminal QA of the Charm v2 TUI found that search matched most resources for short queries, an applied filter became invisible after Enter, the footer dropped every key hint at common widths so the command palette was undiscoverable, modals overflowed the terminal with duplicated selection affordances, and opening the config editor exposed the shell's primary screen.

## What changes

- Match the query fuzzily against resource names and by word prefix against metadata, rank name matches above metadata-only matches, and select the best match when the prior selection no longer matches.
- Keep an applied filter visible with its match count; Esc clears it from the list, and `/` resumes editing the current query.
- Keep command palette, help and quit hints visible in the footer at every supported width, drop other hints individually by priority, and show search-mode hints in the wide layout.
- Lay out the command palette with a separate shortcut column, a match count and hints, and add a clear-search action.
- Render every modal in one bounded frame with a complete border, one focus indicator per option and one hint row.
- Show a waiting screen on the alternate screen while an external config editor runs.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `tui-navigation`: search matching, ranking and applied-filter visibility.
- `tui-feedback`: footer action hints and the command palette's clear-search action.
- `tui-rendering`: modal framing and the editor handoff screen.

## Impact

Changes are confined to `internal/tui`, its tests and `tools/tui-qa`. Configuration, materialization, guarded mutation paths and operation serialization are unchanged.
