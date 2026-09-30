## Why

The TUI uses Charm v1 and custom rendering, modal, and feedback code. Moving to Charm v2 and native components must preserve the terminal, navigation, and guarded-write behavior defined by `docs/tui-product-contract.md` while adding ranked search and truthful operation feedback.

## What changes

- Migrate Bubble Tea, Lip Gloss, Bubbles, and Glamour to pinned Charm v2 modules, preserving shortcuts, paste, terminal restoration, and request generations.
- Use a semantic terminal-palette theme and native composition for rows, modals, footer, tree, and detail presentation.
- Add deterministic fuzzy ranking within existing sections, name-only match highlighting, and vertically scrollable help at 50×12.
- Move modals to Huh forms while preserving scope switches, multi-selection, guarded writes, and cancellation.
- Add a status bar, bounded toasts, observed pipeline phases, and a context-aware command palette using the same guarded action dispatcher as keyboard input.

## Capabilities

### New capabilities

- `tui-navigation`: Grouped resource browsing, filtering, selection anchoring, and keyboard help.
- `tui-rendering`: Theme, responsive composition, sanitization, and terminal behavior.
- `tui-interactions`: Modal selection and mutation behavior with explicit guards.
- `tui-feedback`: Observed operation progress, status, notifications, and command palette actions.

### Modified capabilities

None. Existing TUI behavior lives in `docs/tui-product-contract.md`; the new capabilities capture its relevant baseline alongside the approved changes before archival.

## Impact

Changes touch `internal/tui`, the minimal pipeline observer boundary and its callers, `tools/tui-qa`, Charm dependencies, tests and benchmarks, and the published TUI contract where the new behaviors need documentation. Existing CLI/config/materialization policy, grouped selection semantics, sanitization guarantees, and keyboard-only terminal usage remain unchanged. The design source in the original checkout is untracked and excluded from this change.
