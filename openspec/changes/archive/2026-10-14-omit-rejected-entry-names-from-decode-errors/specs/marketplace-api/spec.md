## ADDED Requirements

### Requirement: Decode rejections omit unsafe entry names

Decoding a resource artifact MUST reject an archive entry whose name is absolute or contains `..` path components without including that entry name in the returned error. The error MUST still state whether the entry was rejected as an absolute path or as path traversal.

#### Scenario: Unsafe entry name is not echoed

- **WHEN** a `tar.gz` whose entry name is absolute or contains `..` path components is decoded
- **THEN** decoding MUST fail with an error identifying the absolute-path or path-traversal rejection
- **AND** the error MUST NOT contain the entry name
