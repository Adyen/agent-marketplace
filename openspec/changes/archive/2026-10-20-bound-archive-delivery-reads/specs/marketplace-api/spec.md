## MODIFIED Requirements

### Requirement: Stored payload reads enforce existing byte ceilings

Public inspection, content, manifest and archive reads using the built-in filesystem and S3 backends SHALL enforce applicable existing inspection-manifest, configured file, manifest and artifact byte ceilings during reading, before fully materializing an oversized stored object. Archive reads cover the final archive served by the artifact route, the placeholder-form archive served or derived by the archive route, and the placeholder-form archive used to verify a final archive. Checking whether an immutable blob already holds the same bytes SHALL read at most one byte more than the bytes being stored. GET and HEAD SHALL retain availability checks, identity verification and their existing success and refusal behavior. This requirement introduces no new ceiling and does not change the existing meaning of an unlimited configured file ceiling or the compatibility interface for custom blob stores.

#### Scenario: Oversized payloads stop at the existing read ceiling

- **WHEN** a public content, inspection-manifest or registry-manifest request encounters a stored object larger than its applicable existing byte ceiling
- **THEN** a built-in backend read SHALL stop after at most that ceiling plus one probe byte
- **AND** GET and HEAD SHALL retain their existing refusal status without immutable success caching

#### Scenario: Healthy payload reads retain response identity

- **WHEN** public content fits its applicable existing byte ceiling
- **THEN** its content bytes, verified digest, media type and immutable success response SHALL remain unchanged

#### Scenario: Oversized archives stop at the artifact ceiling

- **WHEN** the artifact or placeholder archive route reads a final or placeholder-form archive, with or without a stored placeholder
- **THEN** every archive blob read SHALL be bounded by the configured artifact limit, and an archive larger than it SHALL be refused with the existing `413` response without immutable success caching
- **AND** an archive within the limit SHALL still be served with its existing bytes and verification
