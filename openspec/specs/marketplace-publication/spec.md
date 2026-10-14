# marketplace-publication Specification

## Purpose
Defines expected-head validation and verified no-op retry behavior for legacy registry publication, preserving current resource assessments and transactional metadata integrity.

## Requirements

### Requirement: Legacy publication validates its expected head

A legacy registry publication MUST validate its expected effective registry head within the metadata transaction before changing current resource assessments or other publication metadata. A stale expectation, including an empty expectation after initialization, MUST return a conflict unless every requested metadata item is already current and the request is a verified no-op. Refusals MUST preserve all visible metadata. Publication-identity retries SHALL retain their existing contract. A legacy same-commit retry with a matching head expectation SHALL remain valid.

#### Scenario: Stale same-commit assessment is refused

- **GIVEN** a resource revision at the current registry commit has a reject assessment
- **WHEN** a legacy publication for that same commit supplies an allow assessment and an outdated or empty expected head
- **THEN** publication MUST return `store.ErrConflict`
- **AND** the current assessment, registry head and assessment history MUST remain unchanged

#### Scenario: Valid same-commit retry preserves the assessment

- **GIVEN** a legacy publication has initialized the registry head
- **WHEN** the same publication is retried with the matching current expected head
- **THEN** publication SHALL succeed and retain its current assessment

### Requirement: Publication repairs archive-digest drift on identical content

A registry publication that stages a resource revision whose content digest
matches an existing published revision of the same resource but whose archive
digest differs SHALL publish successfully. The publication MUST upload the
newly built resource artifact, update the stored archive digest for that
revision within the publication transaction, and retain the previously
published artifact. License and description SHALL remain immutable for an
unchanged content digest. The publication MUST NOT return a publication
conflict for this case, and the repaired revision MUST serve the new archive
digest to consumers.

#### Scenario: Drifted archive digest republishes without conflict

- **GIVEN** a published resource revision with content digest C and archive
  digest A1
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

### Requirement: Failed refresh attempts record their cause

A failed managed refresh attempt SHALL persist a stable failure code and a
sanitized cause message, and the scheduled worker SHALL log the cause for the
failed attempt. The failure code and cause MUST be exposed on the admin
attempts surface for that registry. The sanitized cause MUST NOT contain raw
command output or credentials.

#### Scenario: Failed publication records the conflict cause

- **WHEN** a managed refresh attempt fails because publication returns a
  conflict
- **THEN** the attempt record MUST carry a stable failure code and a
  sanitized cause identifying the conflict
- **AND** the worker log line for that attempt MUST include the cause
- **AND** the admin attempts surface MUST expose the failure code and cause

#### Scenario: Acquisition failure records a sanitized cause

- **WHEN** a managed refresh attempt fails during acquisition
- **THEN** the attempt record MUST carry a stable failure code and a
  sanitized cause that excludes raw command output

### Requirement: Publication indexes resource prerequisites

Publication SHALL record, for every skill, command, and subagent revision, the prerequisites declared in its `metadata.requires` in declaration order, using the same prerequisite syntax and malformed-declaration handling as catalog discovery. A malformed declaration MUST NOT fail publication. Hook, MCP server, and collection revisions SHALL record no prerequisites. A revision published before prerequisite indexing SHALL remain unindexed until a later publication of the same content records its prerequisites, and an unindexed revision MUST NOT be reported as having no prerequisites. A later publication that records different prerequisites for an already indexed revision MUST be rejected as conflicting immutable metadata. Recorded `registry` and `path` hints MUST be credential-redacted before storage.

#### Scenario: Declared prerequisites are indexed in declaration order

- **WHEN** a published skill declares several valid prerequisites
- **THEN** its revision MUST record them in declaration order with their kinds, names, and hints

#### Scenario: Malformed prerequisite declaration does not fail publication

- **WHEN** a published resource declares a malformed prerequisite
- **THEN** publication MUST succeed
- **AND** the revision MUST record only the prerequisites that catalog discovery accepts

#### Scenario: Revision published before indexing stays unindexed

- **WHEN** a revision was stored before prerequisite indexing existed
- **THEN** the revision MUST be treated as unindexed rather than as declaring no prerequisites
- **AND** a later publication of the same content MUST record its prerequisites

#### Scenario: Conflicting prerequisites for an indexed revision are rejected

- **WHEN** a publication records prerequisites that differ from those already indexed for the same revision
- **THEN** publication MUST reject the conflicting metadata

#### Scenario: Credential-bearing hints are stored redacted

- **WHEN** a published prerequisite has a credential-bearing registry or path hint
- **THEN** the stored hint MUST be credential-redacted
