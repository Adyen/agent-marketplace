## Context

`renderErrors` in `internal/tui/help.go` renders `m.errs` and `m.warnings`. Each line takes its recovery category from `Model.diagnosticCategories()` in `internal/tui/error_recovery.go`. That function previously built a map from stable key to the first category seen. Stable keys come from `pipeline.IssueDedupKey`, which resolves through `errors.As` to the promoted diagnostic's dedup key. A snapshot diagnostic and an operation diagnostic wrapping the same promoted cause therefore share one key even when their severity or category differs.

## Decision

Key categories by `diagnosticCategoryKey{severity, stableKey}` and map each key to an ordered queue of categories. Build the queue by walking snapshot diagnostics then operation diagnostics, matching how `syncDiagnosticViews` projects `m.errs` and `m.warnings`. In `renderErrors`, a local `categoryFor(severity, err)` closure pops the next category for the key. Both the error and warning loops use it. An empty queue yields an empty category, the same as a missing key did before.

Rejected alternatives:

- Track a separate index for each category queue. Consuming the queue directly avoids that extra state.
- Use the diagnostic's own stored `stableKey`. Render lookup uses the error identity instead, so custom or legacy keys cannot shift categories.

## Risks

The queue relies on `m.errs` and `m.warnings` keeping the same order as the diagnostic store. Both come from `diagnosticValues` over the same snapshot-then-operation sequence, so they match. Tests that assign `m.errs` directly get empty categories, as they did before.

Lifetimes, identities, counts, ordering, and write guards are unchanged. Only the category attached to each rendered recovery line changes.
