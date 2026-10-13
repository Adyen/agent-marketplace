## ADDED Requirements

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
