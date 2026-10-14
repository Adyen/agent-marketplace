## ADDED Requirements

### Requirement: Filesystem blob containment

A Marketplace filesystem blob store MUST reject addresses that can select a parent directory, an absolute path, or path separators. Blob reads and mutations MUST remain within the configured storage root, including when an address encounters a symlink. Valid content-addressed layouts and immutable publication conflict behavior MUST remain unchanged.

#### Scenario: Blob addresses cannot escape storage

- **WHEN** a downstream consumer reads, writes, checks, or deletes a blob using an unsafe address
- **THEN** the operation MUST fail before accessing or changing external content
- **AND** an existing valid blob MUST remain readable and unchanged

### Requirement: Active support content scheme policy

An inspection projection MUST classify support text beginning with javascript, data, or vbscript URL schemes as metadata-only, ignoring surrounding whitespace and scheme case. Publication MUST reject a projection that attempts to expose such support bytes as textual content. Safe support text MUST remain available.

#### Scenario: Active URL schemes are metadata only

- **WHEN** an inspection projection contains support text beginning with an active URL scheme and ordinary safe support text
- **THEN** active support bytes MUST not be available as preview content
- **AND** their file identity MUST remain available as metadata
- **AND** ordinary safe support bytes MUST remain available
