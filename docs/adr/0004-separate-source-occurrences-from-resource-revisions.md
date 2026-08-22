---
status: accepted
---

# Separate source occurrences from resource revisions

A resource revision identifies immutable canonical content, while an index-only installation also pins the exact source occurrence from which that content can be reconstructed. Source occurrences are addressed by Source ID and commit through a dedicated API; pins and caches retain the commit and source-manifest digest rather than inferring an occurrence from resource metadata or origin-array order.

## Consequences

Source moves preserve the immutable observed origin but clients clone only the current operator-approved acquisition origin after applying `allowed_registries`. Canonical source manifests exclude mutable origins, refs, and timestamps, so an unchanged commit remains verifiable after a move. Re-ingestion must reproduce the pinned resource-content digest, and failure to obtain the exact commit makes the pin unavailable rather than permitting fallback to another origin or occurrence.
