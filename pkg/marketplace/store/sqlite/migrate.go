package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// This schema is intentionally a new migration epoch. The analyzer-independent
// resource model is not compatible with databases created by the original
// marketplace_schema_migrations epoch.
const migrationTable = "marketplace_metadata_v2_migrations"
const legacyMigrationTable = "marketplace_schema_migrations"

var migrations = []string{migration0}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func execWithBusyRetry(ctx context.Context, db execer, query string, args ...any) error {
	for attempt := 0; ; attempt++ {
		if _, err := db.ExecContext(ctx, query, args...); err == nil {
			return nil
		} else if !isBusy(err) || attempt >= 100 {
			return err
		}
		delay := time.Duration(attempt+1) * 10 * time.Millisecond
		if delay > 100*time.Millisecond {
			delay = 100 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

func runMigrations(ctx context.Context, db *sql.DB) error {
	tx, err := beginMigrationTx(ctx, db)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}

	var legacy bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`,
		legacyMigrationTable,
	).Scan(&legacy); err != nil {
		return rollback(fmt.Errorf("inspect schema epoch: %w", err))
	}
	if legacy {
		return rollback(fmt.Errorf("incompatible legacy SQLite metadata schema %q; create a fresh database for the analyzer-independent metadata schema", legacyMigrationTable))
	}

	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+migrationTable+` (
		version INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return rollback(err)
	}
	for version, migration := range migrations {
		var applied bool
		if err := tx.QueryRowContext(ctx,
			"SELECT EXISTS (SELECT 1 FROM "+migrationTable+" WHERE version = ?)", version,
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
			"INSERT INTO "+migrationTable+" (version, applied_at) VALUES (?, ?)",
			version, time.Now().UTC().Unix(),
		); err != nil {
			return rollback(fmt.Errorf("record migration %d: %w", version, err))
		}
	}
	return tx.Commit()
}

func beginMigrationTx(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	for attempt := 0; ; attempt++ {
		tx, err := db.BeginTx(ctx, nil)
		if err == nil {
			return tx, nil
		}
		if !isBusy(err) || attempt >= 20 {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 25 * time.Millisecond):
		}
	}
}

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "busy") || strings.Contains(message, "locked")
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
    listed INTEGER NOT NULL DEFAULT 1,
    refresh_status TEXT NOT NULL DEFAULT 'unknown',
    refresh_error TEXT,
    refreshed_at INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
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
CREATE INDEX idx_resource_revisions_content ON resource_revisions(marketplace_id, content_digest);

CREATE TABLE registry_occurrences (
    registry_id TEXT NOT NULL,
    ref TEXT NOT NULL,
    roots TEXT NOT NULL DEFAULT '[]',
    revision_commit TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    acquisition TEXT NOT NULL,
    mirrored_artifact_digest TEXT,
    occurred_at INTEGER NOT NULL,
    PRIMARY KEY (registry_id, revision_commit)
);
CREATE INDEX idx_registry_occurrences_registry ON registry_occurrences(registry_id, occurred_at DESC, revision_commit DESC);

CREATE TABLE registry_heads (
    registry_id TEXT PRIMARY KEY,
    revision_commit TEXT NOT NULL,
    FOREIGN KEY (registry_id, revision_commit) REFERENCES registry_occurrences(registry_id, revision_commit)
);

CREATE TABLE resource_occurrences (
    marketplace_id TEXT NOT NULL,
    registry_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    content_digest TEXT NOT NULL,
    revision_commit TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    PRIMARY KEY (marketplace_id, registry_id, kind, name, content_digest, revision_commit),
    FOREIGN KEY (marketplace_id, registry_id, kind, name, content_digest) REFERENCES resource_revisions(marketplace_id, registry_id, kind, name, content_digest),
    FOREIGN KEY (registry_id, revision_commit) REFERENCES registry_occurrences(registry_id, revision_commit)
);
CREATE INDEX idx_resource_occurrences_commit ON resource_occurrences(registry_id, revision_commit, marketplace_id, kind, name);
CREATE INDEX idx_resource_occurrences_revision ON resource_occurrences(marketplace_id, registry_id, kind, name, content_digest);

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
    assessed_at INTEGER NOT NULL,
    PRIMARY KEY (marketplace_id, registry_id, kind, name, content_digest, assessment_digest),
    FOREIGN KEY (marketplace_id, registry_id, kind, name, content_digest) REFERENCES resource_revisions(marketplace_id, registry_id, kind, name, content_digest)
);
CREATE UNIQUE INDEX idx_resource_assessments_digest ON resource_assessments(assessment_digest);
CREATE INDEX idx_resource_assessments_revision ON resource_assessments(marketplace_id, registry_id, kind, name, content_digest, assessed_at DESC);

CREATE TABLE current_resource_assessments (
    marketplace_id TEXT NOT NULL,
    registry_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    content_digest TEXT NOT NULL,
    assessment_digest TEXT NOT NULL,
    PRIMARY KEY (marketplace_id, registry_id, kind, name, content_digest),
    FOREIGN KEY (marketplace_id, registry_id, kind, name, content_digest, assessment_digest) REFERENCES resource_assessments(marketplace_id, registry_id, kind, name, content_digest, assessment_digest)
);

CREATE TABLE collection_revisions (
    id TEXT PRIMARY KEY,
    registry_id TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    members TEXT NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_collection_revisions_registry ON collection_revisions(registry_id);

CREATE TABLE revocations (
    kind TEXT NOT NULL,
    target_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    applied_at INTEGER NOT NULL,
    PRIMARY KEY (kind, target_id)
);
CREATE INDEX idx_revocations_kind_target ON revocations(kind, target_id);

CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    payload BLOB NOT NULL,
    status TEXT NOT NULL,
    lease_until INTEGER,
    attempt INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 1,
    backoff INTEGER NOT NULL DEFAULT 0,
    idempotency_key TEXT,
    worker_id TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_jobs_idempotency ON jobs(kind, idempotency_key) WHERE idempotency_key <> '';
CREATE INDEX idx_jobs_lease ON jobs(status, lease_until, kind, attempt, max_attempts, created_at);
`
