## ADDED Requirements

### Requirement: Resource revision source provenance
Resource revision responses for the latest resource, revision lists, and exact revisions SHALL include an optional `source` object with the same shape and sanitization as the catalog `CatalogSource`: a safe HTTP(S) `repository_url`, the normalized repository-relative `path` of the resource in that occurrence, and the exact occurrence `commit`. The response SHALL omit `source` when the occurrence has no recorded source path or source origin, or when the origin is not a safe HTTP(S) URL.

#### Scenario: Resource revision exposes sanitized source provenance
- **WHEN** a client fetches a resource revision whose occurrence recorded a source origin and source path
- **THEN** the response `source` carries the repository URL with userinfo and credential-like query values removed, the normalized path, and the occurrence commit

#### Scenario: Resource revision omits untrustworthy source provenance
- **WHEN** a resource revision has no recorded source path or source origin, or its origin is not a safe HTTP(S) URL
- **THEN** the response omits `source`
