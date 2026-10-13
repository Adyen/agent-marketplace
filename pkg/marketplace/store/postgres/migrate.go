package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var migrations = []string{migration0, migration1, migration2, store.ManagedConfigurationSchema, store.PublicationSchema, store.BlobOwnershipMigration(true), store.ManagedRuntimeSchema, store.ManagedPreviewSchema, migrationDigestIndexes, migrationRequires}

const migrationTable = "marketplace_metadata_v2_schema_migrations"

func runMigrations(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"SELECT pg_advisory_xact_lock(hashtext($1)::bigint)",
		"agent-marketplace:metadata-v2-schema-migrations",
	); err != nil {
		return rollback(fmt.Errorf("acquire migration lock: %w", err))
	}

	var legacy, current bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('marketplace_schema_migrations') IS NOT NULL,
		to_regclass($1) IS NOT NULL`, migrationTable).Scan(&legacy, &current); err != nil {
		return rollback(fmt.Errorf("inspect metadata schema epoch: %w", err))
	}
	if legacy && !current {
		return rollback(fmt.Errorf("legacy PostgreSQL metadata schema detected; create a fresh database or schema for the analyzer-independent metadata model"))
	}

	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+migrationTable+` (
		version INTEGER PRIMARY KEY,
		applied_at BIGINT NOT NULL
	)`); err != nil {
		return rollback(err)
	}
	for version, migration := range migrations {
		var applied bool
		if err := tx.QueryRowContext(ctx,
			"SELECT EXISTS (SELECT 1 FROM "+migrationTable+" WHERE version = $1)", version,
		).Scan(&applied); err != nil {
			return rollback(fmt.Errorf("check migration %d: %w", version, err))
		}
		if applied {
			continue
		}
		if _, err := tx.ExecContext(ctx, migration); err != nil {
			return rollback(fmt.Errorf("migration %d: %w", version, err))
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO "+migrationTable+" (version, applied_at) VALUES ($1, $2)",
			version, time.Now().UTC().Unix(),
		); err != nil {
			return rollback(fmt.Errorf("record migration %d: %w", version, err))
		}
	}
	return tx.Commit()
}

const migration0 = `
CREATE TABLE marketplaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    canonical_url TEXT NOT NULL,
    instance_metadata TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE registries (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL,
    aliases TEXT NOT NULL DEFAULT '[]',
    origins TEXT NOT NULL DEFAULT '[]',
    ref TEXT NOT NULL,
    roots TEXT NOT NULL DEFAULT '[]',
    acquisition TEXT NOT NULL,
    publisher TEXT NOT NULL DEFAULT '{}',
    license TEXT,
    credentials_ref TEXT,
    policy_overrides TEXT NOT NULL DEFAULT '{}',
    listed BOOLEAN NOT NULL DEFAULT true,
    refresh_status TEXT NOT NULL DEFAULT 'unknown',
    refresh_error TEXT,
    refreshed_at BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);
CREATE UNIQUE INDEX idx_registries_slug ON registries(slug);

CREATE TABLE resource_revisions (
    marketplace_id TEXT NOT NULL,
    registry_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    content_digest TEXT NOT NULL,
    archive_digest TEXT,
    license TEXT,
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (marketplace_id, registry_id, kind, name, content_digest)
);
CREATE INDEX idx_resource_revisions_archive ON resource_revisions(marketplace_id, archive_digest);

CREATE TABLE resource_occurrences (
    marketplace_id TEXT NOT NULL,
    registry_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    content_digest TEXT NOT NULL,
    revision_commit TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    PRIMARY KEY (marketplace_id, registry_id, kind, name, content_digest, revision_commit),
    FOREIGN KEY (marketplace_id, registry_id, kind, name, content_digest)
        REFERENCES resource_revisions(marketplace_id, registry_id, kind, name, content_digest)
);
CREATE INDEX idx_resource_occurrences_head ON resource_occurrences(registry_id, revision_commit, kind, name);

CREATE TABLE resource_assessments (
    marketplace_id TEXT NOT NULL,
    registry_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    content_digest TEXT NOT NULL,
    assessment_digest TEXT NOT NULL,
    analyzer_version TEXT NOT NULL,
    policy_digest TEXT NOT NULL,
    decision TEXT NOT NULL,
    findings TEXT NOT NULL DEFAULT '[]',
    inspection_manifest_digest TEXT NOT NULL,
    assessed_at BIGINT NOT NULL,
    PRIMARY KEY (marketplace_id, registry_id, kind, name, content_digest, assessment_digest),
    FOREIGN KEY (marketplace_id, registry_id, kind, name, content_digest)
        REFERENCES resource_revisions(marketplace_id, registry_id, kind, name, content_digest)
);

CREATE UNIQUE INDEX idx_resource_assessments_digest ON resource_assessments(assessment_digest);

CREATE TABLE current_resource_assessments (
    marketplace_id TEXT NOT NULL,
    registry_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    content_digest TEXT NOT NULL,
    assessment_digest TEXT NOT NULL,
    PRIMARY KEY (marketplace_id, registry_id, kind, name, content_digest),
    FOREIGN KEY (marketplace_id, registry_id, kind, name, content_digest, assessment_digest)
        REFERENCES resource_assessments(marketplace_id, registry_id, kind, name, content_digest, assessment_digest)
);

CREATE TABLE registry_occurrences (
    registry_id TEXT NOT NULL,
    ref TEXT NOT NULL,
    roots TEXT NOT NULL DEFAULT '[]',
    revision_commit TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    acquisition TEXT NOT NULL,
    mirrored_artifact_digest TEXT,
    occurred_at BIGINT NOT NULL,
    PRIMARY KEY (registry_id, revision_commit)
);
CREATE INDEX idx_registry_occurrences_registry ON registry_occurrences(registry_id, occurred_at);

CREATE TABLE registry_heads (
    registry_id TEXT PRIMARY KEY,
    revision_commit TEXT NOT NULL,
    FOREIGN KEY (registry_id, revision_commit) REFERENCES registry_occurrences(registry_id, revision_commit)
);

CREATE TABLE collection_revisions (
    id TEXT PRIMARY KEY,
    registry_id TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    members TEXT NOT NULL DEFAULT '[]',
    created_at BIGINT NOT NULL
);
CREATE INDEX idx_collection_revisions_registry ON collection_revisions(registry_id);

CREATE TABLE revocations (
    kind TEXT NOT NULL,
    target_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    applied_at BIGINT NOT NULL,
    PRIMARY KEY (kind, target_id)
);
CREATE INDEX idx_revocations_kind_target ON revocations(kind, target_id);

CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    payload BYTEA NOT NULL,
    status TEXT NOT NULL,
    lease_until BIGINT,
    attempt INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 1,
    backoff BIGINT NOT NULL DEFAULT 0,
    idempotency_key TEXT,
    worker_id TEXT,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);
CREATE UNIQUE INDEX idx_jobs_idempotency ON jobs(kind, idempotency_key) WHERE idempotency_key <> '';
CREATE INDEX idx_jobs_lease ON jobs(status, lease_until, kind, attempt, max_attempts, created_at);
`

// migration1 adds nullable occurrence provenance and the single durable
// catalog version row without rewriting legacy metadata.
const migration1 = `
ALTER TABLE registry_occurrences ADD COLUMN source_origin TEXT;
ALTER TABLE resource_occurrences ADD COLUMN source_path TEXT;

CREATE TABLE catalog_versions (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    version BIGINT NOT NULL
);
INSERT INTO catalog_versions (singleton, version) VALUES (1, 0);
`

// migration2 preserves the direct collection include declaration separately
// from direct resource members.
const migration2 = `
ALTER TABLE collection_revisions ADD COLUMN includes TEXT NOT NULL DEFAULT '[]';
`

const migrationDigestIndexes = `
CREATE INDEX idx_resource_revisions_content ON resource_revisions(marketplace_id, content_digest);
CREATE INDEX idx_current_assessments_content ON current_resource_assessments(marketplace_id, content_digest);
`

// migrationRequires adds the nullable per-revision prerequisite column. NULL
// marks revisions published before prerequisite indexing; no backfill.
const migrationRequires = `
ALTER TABLE resource_revisions ADD COLUMN requires TEXT;
`
