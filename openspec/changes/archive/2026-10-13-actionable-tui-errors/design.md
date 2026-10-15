## Context

`renderErrors` in `internal/tui/help.go` prints each error and warning, then one recovery line. The category for a rendered error lives in the diagnostic store, keyed by its stable key.

## Decisions

- `diagnosticRemediation(err, category)` in `internal/tui/error_recovery.go` chooses the hint in this order: nonempty `Remediable.Remediation()`, no hint for the verification sentinel, typed recovery identities, then the category default.
- `Model.diagnosticCategories()` builds a key-to-category map once per render from the snapshot and operation diagnostics, keeping first-match order. Each rendered error does one lookup, and unmatched errors get the empty category (the unclassified default). The map is local to the render, with no cached state on the model.
- Discovery errors use the catalog category.
- The verification fallback is the sentinel `errResourceStateUnverified`, whose text is the recovery copy itself. Rendering it adds no second hint.

## Risks

Discovery errors wrap `marketplace.EndpointError`, which implements `Unwrap`, so `errors.Is` and `errors.As` reach typed causes.
