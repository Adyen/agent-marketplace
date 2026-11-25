# Tasks

## 1. Empty diagnostics copy behavior

- [x] 1.1 Add a spec-bound workspace regression for empty diagnostics using both clipboard routes; verify it fails for the missing disabled state with `./node_modules/.bin/vitest run src/workspace.test.ts` from `gui/`.
- [x] 1.2 Disable Copy diagnostics for an empty list and verify the focused workspace test passes with the same Vitest command.
- [x] 1.3 Run GUI typecheck, full Vitest suite, Vite build, targeted feedback Playwright test, and `make validate`; retain their logs under `/tmp/pr1050-review-evidence/`.

## Workflow follow-up

- Archived as `2026-11-13-disable-empty-gui-diagnostics-copy` after all three tracked tasks were complete.
- Pinned OpenSpec 1.12.0 strict validation passed (87/87); `specaudit trace` passed (1669 scenarios, 2972 annotations); `specaudit archived` passed (2 active changes).
- The `feedback.spec.ts` run had 6 passes and 1 source-tooltip failure. The isolated `source tooltips show` case failed on baseline `f990cb78621964c5c724349313e7c4ce747594f0` as well; this does not claim that the full browser gate passes.
