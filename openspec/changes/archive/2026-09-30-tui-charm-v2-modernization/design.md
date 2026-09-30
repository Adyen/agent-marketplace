## Context

The TUI currently uses Bubble Tea v1, Lip Gloss v1, Bubbles v1 and Glamour v1 with custom overlays, row layout, footer width fitting, modals and help. `docs/tui-product-contract.md` is authoritative for existing terminal safety, operation generation, write serialization and selection behavior. The approved implementation covers all five modernization slices in one MR to `snappy-tui`; the original checkout's untracked planning document is not included.

## Goals and non-goals

Implement the behavior in the four TUI capability specs without weakening the existing product contract. Preserve the `flatItems` grouped browse and fold model, request generations, post-write authoritative reload, minimum size and keyboard-only operation. Do not change CLI/config/materialization policy, enable mouse tracking, or infer live progress from completed failure classes.

## Decisions

1. **Migrate before visual changes.** Pin `charm.land/bubbletea/v2` v2.0.9, `lipgloss/v2` v2.0.6, `bubbles/v2` v2.2.1, `huh/v2` v2.0.3 and `glamour/v2` v2.0.1. Migrate imports, key messages, `tea.View`, background message and paste routing. Space is `space` in v2, and bracketed paste arrives as `PasteMsg`, so preserving v1 string tests without adapting semantics would break shortcuts. Keep the outer terminal guard and bounded width-keyed Markdown cache.
2. **Keep the model, replace presentation.** Build semantic Lip Gloss roles on ANSI 0–15 with a deterministic test renderer; only color-ambiguous cases need background detection. Shared row badges, scope chips, status bar and key hints are render functions. Use Lip Gloss layers for base view, Huh forms, toasts and palette. Delete custom line-splicing overlay and footer fitters only after all responsive and security tests pass. Do not adopt `bubbles/list`: it does not preserve existing variable-height fold and anchoring behavior without re-implementing the model.
3. **Score where display order is set.** Apply fuzzy matching over deterministic searchable metadata; carry scores and matched name indices through flat item rebuilding into the final leaf-section sort. Preserve section order and restore folds/alphabetic order on clear. Sanitize before mapping match indices into visible names; metadata-only matches never highlight the name. Use native tree/table rendering for bundle/details and render help groups separately in a vertical viewport, since a horizontal FullHelpView call can hide later groups at 50 columns.
4. **Route keys by modal intent.** Create an owned Huh form per modal. Handle cancel, hard quit, scope switches and letter accelerators before forwarding remaining keys to the form. MultiSelect supports init and harness editing; Enter is the only saving action. Keep existing guarded mutation commands and completion messages. A generic accelerator-submit rule was rejected because scope switches must not write.
5. **Observe actual pipeline work.** Extend the minimal lifecycle boundary with an optional phase observer usable by both direct and injected pipeline paths, independent of Tea messages. Adapters forward identity, sequence and request generation into `Update`; only accepted events change display state. Writes and authoritative reload retain completion authority. An observerless path displays generic running, not guessed phases.
6. **One action authority.** Define stable palette action identities with current-state eligibility and reuse the same guarded dispatcher from keyboard actions. Prefix actions use identities, not synthetic key sequences. A form and the palette cannot authorize a write merely by having been opened; recheck on submission. Toasts are bounded and timer-driven; the status bar reads state and never owns diagnostics.

## Risks and trade-offs

- V2 changes key encoding and paste delivery. Run focused Space/paste tests and all direct terminal QA journeys before and after visual changes.
- Native components can shift rows or conceal actions at 50×12. Keep anchoring, sanitization and bounds assertions and verify every help group by scrolling.
- Fuzzy ranking can be silently erased by downstream alphabetical sorting. Assert final displayed order for each tab, ties, metadata order and remote arrival.
- Huh can swallow quit or transform a scope switch into a write. Explicit adapter routing and byte-exact no-write tests guard these paths.
- Late progress can claim success after completion. Correlate generation and monotonic sequence; preserve completion authority and assert no diagnostic changes.
- Charm migration touches hot render and input paths. Compare repeated base/head benchmarks on identical fixtures and report allocations.

## Migration plan

Implement the five slices serially inside this one feature branch. Begin each changed behavior with a failing test, then migrate dependencies and input behavior, theme/composition, browsing/help, modals, and feedback/palette. Update QA journeys and benchmark coverage in the same candidate. Archive the OpenSpec change to publish specs before final review. Verify `make validate`, `make spec-check`, `make tui-qa`, strict OpenSpec validation and relevant native gates on the clean final commit. Rollback is a revert of the single feature MR; no persisted schema or data migration occurs.
