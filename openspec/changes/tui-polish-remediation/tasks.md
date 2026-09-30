## 1. Search

- [ ] 1.1 Write failing tests for name-fuzzy and metadata word-prefix matching, name-first ranking, best-match selection, the applied-filter chip, Esc clearing from the list and `/` refining; implement and verify green.

## 2. Footer and palette

- [ ] 2.1 Write failing tests that the palette, help and quit hints stay visible at 50, 80 and 120 columns with warnings and active operations, and that wide search mode shows its hints; implement per-binding footer groups.
- [ ] 2.2 Add the palette shortcut column, count, hints and clear-search action with eligibility tests.

## 3. Modals and editor

- [ ] 3.1 Write failing width, border and single-indicator tests for every modal at 50×12, 80×24 and 120×34; implement the shared modal frame.
- [ ] 3.2 Write a failing editor handoff test for the waiting screen; implement it without changing editor signal handling.

## 4. Delivery evidence

- [ ] 4.1 Add a layout-quality TUI QA journey and update affected journeys; verify `make tui-qa`, `make spec-check` and `make validate`.
