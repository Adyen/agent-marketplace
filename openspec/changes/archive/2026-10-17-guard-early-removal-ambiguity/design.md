## Decision

Use effective direct selections from validated configuration for early removal
admission. This includes missing or malformed names without requiring discovery
to succeed. Reuse existing kind/source/scope lookup and keep these candidates
separate from materializable resolver output.

Only activate the admission when an explicitly selected registry resource
competes with a collection or Marketplace selection. Ordinary resolver winner
selection and Marketplace-only lookup retain their existing behavior. A scoped
choice must also restrict early dispatch so another layer is not edited first.

Named `--from` already identifies one registry entry; apply existing
registry-scoped kind lookup before its collection branch, and never enter that
branch for an explicit non-collection kind.

Moving full resolution ahead of every early return would make stale collection
cleanup depend on discovery and acquisition. Considering every include in the
ordinary removal path would change collision-winner behavior. Both are avoided.

## Verification

Prove unqualified refusal and explicit-kind success for collections and
Marketplace pins, including named `--from`. Preserve configuration and active
collection output on refusal. Assert exact parent-registry selection survivors.
Exercise refusal, kind disambiguation, and single-selection removal through
the public CLI.
