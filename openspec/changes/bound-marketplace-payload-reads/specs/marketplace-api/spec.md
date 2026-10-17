## ADDED Requirements

### Requirement: Stored payload reads enforce existing byte ceilings

Public inspection, content and manifest reads using the built-in filesystem and S3 backends SHALL enforce applicable existing inspection-manifest, configured file and manifest byte ceilings during reading, before fully materializing an oversized stored object. GET and HEAD SHALL retain availability checks, identity verification and their existing success and refusal behavior. This requirement introduces no new ceiling and does not change the existing meaning of an unlimited configured file ceiling or the compatibility interface for custom blob stores.

#### Scenario: Oversized payloads stop at the existing read ceiling

- **WHEN** a public content, inspection-manifest or registry-manifest request encounters a stored object larger than its applicable existing byte ceiling
- **THEN** a built-in backend read SHALL stop after at most that ceiling plus one probe byte
- **AND** GET and HEAD SHALL retain their existing refusal status without immutable success caching

#### Scenario: Healthy payload reads retain response identity

- **WHEN** public content fits its applicable existing byte ceiling
- **THEN** its content bytes, verified digest, media type and immutable success response SHALL remain unchanged
