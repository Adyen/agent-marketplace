---
status: accepted
---

# Compute effective revocation from live desired state

Operator revocation configuration is authoritative desired state reconciled transactionally before API readiness, while resource revisions remain immutable. Effective revocation is overlaid during reads and resolution: any active source, coordinate, or coordinate-plus-digest declaration revokes, with specificity selecting the primary reason rather than exempting a revision from a broader declaration.

## Consequences

Removing a declaration restores ordinary resolution after reconciliation while retaining audit history. Revoked metadata, history, and sanitized inspection remain visible, but ordinary resolution and new artifact distribution stop. Offline sync enforces the last cached observation, blocks known-revoked pins, and warns when a cached non-revoked observation may be stale.
