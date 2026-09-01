---
status: accepted
---

# Separate registry occurrences from resource revisions

A resource revision identifies immutable canonical content, while an index-only installation also pins the exact registry occurrence from which that content can be reconstructed. A resource occurrence records the revision's membership in that registry occurrence. Registry occurrences are addressed by Registry ID and commit through a dedicated API; pins and caches retain the commit and registry-manifest digest, while the occurrence retains the exact registry roots needed for reconstruction rather than inferring an occurrence from resource metadata or origin-array order. Analyzer and operator-policy output belongs to separately versioned resource assessments and cannot change resource, artifact, inspection-projection, or registry-occurrence identity.

## Consequences

Registry moves preserve the immutable observed origin but clients clone only the current operator-approved acquisition origin after applying `allowed_registries`. Canonical registry manifests exclude mutable origins, refs, timestamps, analyzer versions, findings, and policy decisions, so an unchanged commit remains verifiable after a move or reassessment. Re-ingestion must reproduce the pinned resource-content digest, and failure to obtain the exact commit makes the pin unavailable rather than permitting fallback to another origin or occurrence.
