# Spec Delta

## ADDED Requirements

### Requirement: Registry configuration changes apply atomically

The metadata store SHALL apply all registry record and desired-state changes of one Marketplace operator configuration candidate in one transaction: registry records are created, updated, or unlisted, missing observation records are created, and desired indexing generations and next observation times are set together. If any change fails, the store MUST leave every registry record and observation record exactly as it was before the candidate. SQLite and PostgreSQL SHALL behave identically.

#### Scenario: A failed configuration change applies nothing

- **WHEN** a configuration candidate that adds one registry, changes the desired generation of another, and unlists a third fails while writing its last change
- **THEN** no registry is added, the second registry's observation keeps its previous desired generation and version, the third registry stays listed, and the same candidate applied again succeeds completely

### Requirement: Publication requires the current desired generation

A registry revision publication that names an expected indexing generation SHALL succeed only when the registry's observation record holds that generation as desired at the moment the publication commits. Otherwise publication MUST fail with a stale-generation conflict and write no registry occurrence, resource revision, resource occurrence, resource assessment, collection revision, or head change. SQLite and PostgreSQL SHALL behave identically.

#### Scenario: Publication for a generation that is no longer desired is rejected

- **WHEN** a registry revision is published naming an expected generation after the registry's desired generation has changed to a different value
- **THEN** publication fails with a stale-generation conflict, the registry head is unchanged, and none of the publication's records exist
