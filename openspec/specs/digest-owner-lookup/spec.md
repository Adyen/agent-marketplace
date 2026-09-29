# digest-owner-lookup Specification

## Purpose

Requires digest-scoped indexes in every supported Marketplace metadata store
backend, so a store cannot drop the indexes that digest-owner resolution
depends on without a spec review.

## Requirements

### Requirement: Digest-owner lookup indexes

A Marketplace metadata store SHALL maintain a non-unique index over
(Marketplace, content digest) on its resource-revision relation, and on its
current-assessment relation when the backend stores current assessments
separately. No pre-existing digest index SHALL be removed or altered by this
contract.

#### Scenario: PostgreSQL store keeps digest-owner indexes

- **WHEN** a PostgreSQL Marketplace metadata store is opened and migrated to
  the current schema
- **THEN** a non-unique index over `(marketplace_id, content_digest)` MUST
  exist on `resource_revisions`
- **AND** a non-unique index over `(marketplace_id, content_digest)` MUST
  exist on `current_resource_assessments`

#### Scenario: SQLite store keeps digest-owner index

- **WHEN** a SQLite Marketplace metadata store is opened and migrated to the
  current schema
- **THEN** an index over `(marketplace_id, content_digest)` MUST exist on
  `current_resource_assessments`

#### Scenario: Pre-existing SQLite revision index is preserved

- **WHEN** a SQLite Marketplace metadata store is opened and migrated to the
  current schema
- **THEN** `resource_revisions` MUST retain exactly one index whose
  definition covers `content_digest`
