---
status: accepted
---

# Separate source occurrences from resource revisions

A resource revision identifies immutable canonical content, while an index-only installation also pins the exact source occurrence from which that content can be reconstructed. A resource occurrence records the revision's membership in that source occurrence. Source occurrences are addressed by Source ID and commit through a dedicated API; pins and caches retain the commit and source-manifest digest, while the occurrence retains the exact source roots needed for reconstruction rather than inferring an occurrence from resource metadata or origin-array order. Analyzer and operator-policy output belongs to separately versioned resource assessments and cannot change resource, artifact, inspection-projection, or source-occurrence identity.

## Consequences

Source moves preserve the immutable observed origin but clients clone only the current operator-approved acquisition origin after applying `allowed_registries`. Canonical source manifests exclude mutable origins, refs, timestamps, analyzer versions, findings, and policy decisions, so an unchanged commit remains verifiable after a move or reassessment. Re-ingestion must reproduce the pinned resource-content digest, and failure to obtain the exact commit makes the pin unavailable rather than permitting fallback to another origin or occurrence.
