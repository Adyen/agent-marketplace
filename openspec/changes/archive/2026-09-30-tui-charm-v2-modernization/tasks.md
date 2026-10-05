## 1. Charm v2 foundation

- [x] 1.1 Add regression tests for Space toggle, harness selection, focused paste, remote debounce, and suspended input; verify failures on a partially migrated v2 tree and green on the complete migration (base red exempt by user approval).
- [x] 1.2 Pin Charm v2 dependencies and migrate Bubble Tea input, View and background detection; verify existing `internal/tui` assertions and new key/paste tests pass.
- [x] 1.3 Preserve terminal guard, renderer cache, minimum size and async generation behavior through v2; verify terminal guard, size and security tests and all existing TUI QA journeys.

## 2. Theme and native rendering

- [x] 2.1 Write render tests for semantic ANSI and no-color roles, status symbols and scope chips; verify intended red on base and green after theme/component implementation.
- [x] 2.2 Replace inline colors, overlay and footer fitting with themed components and Lip Gloss layer composition; verify responsive and untrusted-terminal-data tests at 50×12.
- [x] 2.3 Render bundle tree and detail key/value sections with native components while preserving selection and previews; verify bundle and detail behavior tests.

## 3. Browsing and help

- [x] 3.1 Write failing final-tree ordering and highlight tests for ranked fuzzy leaves, deterministic metadata/ties, remote arrival, and clear/fold restoration; verify intended red on base.
- [x] 3.2 Implement fuzzy scoring, ranked section rendering and safe name-only highlighting; verify final rendered order and selection anchoring in every tab.
- [x] 3.3 Write failing narrow-help reachability tests, then stack help groups in the persistent viewport; verify every binding can be reached and offset survives reopen at 50×12.

## 4. Forms and mutation safety

- [x] 4.1 Write failing multi-harness, scope-switch/no-write, unknown-ID and cancel tests; verify intended red on base.
- [x] 4.2 Replace harness/init and remaining modals with per-modal Huh forms and intent routing; verify Enter-only saves, guarded completions, no-write paths and real-terminal journeys.

## 5. Feedback and action authority

- [x] 5.1 Write failing blocked-phase, stale/duplicate event, generic-running and authoritative-reload tests for direct and injected lifecycle paths; verify intended red on base.
- [x] 5.2 Implement optional pipeline phase observer and correlated TUI status, bounded toasts and timer expiry; verify progress cannot finish work or clear diagnostics.
- [x] 5.3 Write failing palette context, current-state eligibility, prefix-action and keyboard parity tests; verify intended red on base.
- [x] 5.4 Implement context-aware fuzzy palette with stable actions and shared guarded keyboard dispatch; verify no modal-only actions and stale eligibility never bypasses confirmation.

## 6. Delivery evidence

- [x] 6.1 Extend all affected real-terminal QA journeys and hot-path benchmarks; verify 13 journeys and report repeated identical-fixture base/head allocation comparisons.
- [x] 6.2 Reconcile published TUI product contract and OpenSpec specs with implemented behavior; verify `openspec validate tui-charm-v2-modernization --strict`, `make spec-check`, and `make validate`.
