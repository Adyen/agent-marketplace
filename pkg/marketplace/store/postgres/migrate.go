package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// migrations contains the complete schema for the PostgreSQL metadata adapter.
// Additional migrations can be appended to this slice over time.
var migrations = []string{migration0, migration1, migration2, migration3}

const migrationTable = "marketplace_schema_migrations"

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
		"agent-marketplace:metadata-schema-migrations",
	); err != nil {
		return rollback(fmt.Errorf("acquire migration lock: %w", err))
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
CREATE TABLE IF NOT EXISTS marketplaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    canonical_url TEXT NOT NULL,
    instance_metadata TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS sources (
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
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sources_slug ON sources(slug);

CREATE TABLE IF NOT EXISTS resource_revisions (
    marketplace_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    content_digest TEXT NOT NULL,
    archive_digest TEXT,
    analyzer_version TEXT,
    findings TEXT NOT NULL DEFAULT '[]',
    license TEXT,
    provenance TEXT,
    revocation TEXT,
    PRIMARY KEY (marketplace_id, source_id, kind, name, content_digest)
);

CREATE INDEX IF NOT EXISTS idx_resource_revisions_coord
    ON resource_revisions(source_id, kind, name);

CREATE TABLE IF NOT EXISTS source_occurrences (
    source_id TEXT NOT NULL,
    ref TEXT NOT NULL,
    revision_commit TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    acquisition TEXT NOT NULL,
    mirrored_artifact_digest TEXT,
    occurred_at BIGINT NOT NULL,
    PRIMARY KEY (source_id, revision_commit)
);

CREATE INDEX IF NOT EXISTS idx_source_occurrences_source
    ON source_occurrences(source_id, occurred_at);

CREATE TABLE IF NOT EXISTS collection_revisions (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    members TEXT NOT NULL DEFAULT '[]',
    created_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_collection_revisions_source
    ON collection_revisions(source_id);

CREATE TABLE IF NOT EXISTS revocations (
    kind TEXT NOT NULL,
    target_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    applied_at BIGINT NOT NULL,
    PRIMARY KEY (kind, target_id)
);

CREATE INDEX IF NOT EXISTS idx_revocations_kind_target
    ON revocations(kind, target_id);

CREATE TABLE IF NOT EXISTS jobs (
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_idempotency
    ON jobs(kind, idempotency_key)
    WHERE idempotency_key <> '';

CREATE INDEX IF NOT EXISTS idx_jobs_lease
    ON jobs(status, lease_until, kind, attempt, max_attempts, created_at);
`

const migration1 = `
ALTER TABLE sources ADD COLUMN IF NOT EXISTS refresh_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE sources ADD COLUMN IF NOT EXISTS refresh_error TEXT;
ALTER TABLE sources ADD COLUMN IF NOT EXISTS refreshed_at BIGINT NOT NULL DEFAULT 0;
`

const migration2 = `
ALTER TABLE resource_revisions ADD COLUMN IF NOT EXISTS created_at BIGINT NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_resource_revisions_created
    ON resource_revisions(marketplace_id, source_id, kind, name, created_at);
`

// migration3 mirrors the SQLite adapter's description column. Existing rows
// keep their data and take the empty default, which round-trips to an omitted
// description in API responses.
const migration3 = `
ALTER TABLE resource_revisions ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
`
