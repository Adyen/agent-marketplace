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
A stored revision without a stored placeholder-form archive SHALL NOT be reused,
because its final archive cannot be verified without re-encoding; the newly built
archive and its placeholder are published instead.

#### Scenario: Identical content republished under another toolchain keeps its archive digest

- **GIVEN** a stored resource revision built by one Go toolchain with its placeholder-form archive stored
- **WHEN** a publication from another Go toolchain stages the same content
- **THEN** publication MUST succeed
- **AND** the stored archive digest MUST be reused
- **AND** the registry revision manifest digest MUST be the one derived from the reused archive digest

#### Scenario: Stored archive without a placeholder is not reused

- **GIVEN** a stored resource revision with no stored placeholder-form archive whose archive digest differs from the newly built archive
- **WHEN** a publication stages the same content
- **THEN** publication MUST succeed
- **AND** the revision MUST carry the newly built archive digest and its placeholder-form archive

#### Scenario: Drifted content still publishes a new archive

- **GIVEN** a stored resource revision whose manifest differs from the newly built artifact ignoring its archive digest
- **WHEN** a publication stages that resource
- **THEN** the newly built archive digest MUST be used
- **AND** the existing archive-digest repair scenario MUST apply

## MODIFIED Requirements

### Requirement: Publication repairs archive-digest drift on identical content

A registry publication that stages a resource revision whose content digest
matches an existing published revision of the same resource but whose archive
digest differs, and whose stored archive cannot be reused under "Republishing
identical content reuses the stored archive", SHALL publish successfully. The
publication MUST upload the newly built resource artifact, update the stored
archive digest for that revision within the publication transaction, and
retain the previously published artifact. License and description SHALL remain
immutable for an unchanged content digest. The publication MUST NOT return a
publication conflict for this case, and the repaired revision MUST serve the
new archive digest to consumers.

#### Scenario: Drifted archive digest republishes without conflict

- **GIVEN** a published resource revision with content digest C and archive
  digest A1
- **AND** the stored archive cannot be reused because the stored revision has
  no stored placeholder-form archive or the newly built artifact differs from
  it ignoring the archive digest
- **WHEN** a publication stages the same resource with content digest C and
  archive digest A2
- **THEN** publication MUST succeed
- **AND** the revision's stored archive digest MUST be A2
- **AND** the artifact content-addressed by A1 MUST remain downloadable
- **AND** the revision's license and description MUST remain unchanged

#### Scenario: Drifted license or description still conflicts

- **GIVEN** a published resource revision with content digest C
- **WHEN** a publication stages the same resource with content digest C and
  a different license or description
- **THEN** publication MUST return a conflict
- **AND** the stored revision MUST remain unchanged
