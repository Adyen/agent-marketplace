## Context

The audit already retains failures, findings, observed evidence, and digests.
The proposal independently writes baseline refreshes and catalog stubs, so
documentation drift alone currently passes the workflow's dirty-tree gate.

## Decisions

Remove automatic baseline writes from proposals and label fingerprint changes
as evidence requiring review. Keep actual model-stub generation and its
repository-root confinement. Gate workflow publication on catalog changes and
stage only catalogs. Preserve existing branch ownership and lease protections.

The detailed rationale and offline verification approach are in
`docs/plans/2026-10-20-actionable-catalog-proposals-design.md`.

## Risks

Documentation-only drift intentionally continues to fail the audit until a
person reviews the source and updates the baseline. Manual-only model candidates
remain alerts rather than empty or hash-only PRs. Existing open drift PRs are not
automatically edited or closed by a no-change run.
