## ADDED Requirements

### Requirement: Publication stores the placeholder archive with the final archive

A registry publication that stages a mirror resource artifact SHALL store its
placeholder-form archive under the placeholder key derived from the archive
digest, alongside the final archive. A placeholder archive whose bytes do not
hash to the digest it is keyed by MUST be refused without changing metadata.

#### Scenario: Publication stores placeholder with final archive

- **WHEN** a mirror registry publication stages a resource artifact
- **THEN** the publication MUST store the final archive under its archive digest
- **AND** the placeholder-form archive under its placeholder key
- **AND** a publication whose placeholder bytes do not hash to the archive digest MUST be refused

### Requirement: Republishing identical content reuses the stored archive

A registry publication that stages a resource whose content digest already has
a stored revision SHALL reuse that revision's archive digest and final archive
when the newly built artifact is equal to the stored one ignoring its archive
digest. The reused archive MUST NOT be re-encoded, and the registry revision
manifest digest, occurrence, and resource occurrences MUST be rebuilt to carry
the reused archive digest.

#### Scenario: Identical content republished under another toolchain keeps its archive digest

- **GIVEN** a stored resource revision built by one Go toolchain
- **WHEN** a publication from another Go toolchain stages the same content
- **THEN** publication MUST succeed
- **AND** the stored archive digest MUST be reused
- **AND** the registry revision manifest digest MUST be the one derived from the reused archive digest

#### Scenario: Drifted content still publishes a new archive

- **GIVEN** a stored resource revision whose manifest differs from the newly built artifact ignoring its archive digest
- **WHEN** a publication stages that resource
- **THEN** the newly built archive digest MUST be used
- **AND** the existing archive-digest repair scenario MUST apply
