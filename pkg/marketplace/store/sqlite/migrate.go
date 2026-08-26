package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// migrations contains the complete schema for the SQLite metadata adapter.
// Additional migrations can be appended to this slice over time.
var migrations = []string{migration0, migration1, migration2, migration3}

const migrationTable = "marketplace_schema_migrations"

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
	var tx *sql.Tx
	var err error
	for attempt := 0; ; attempt++ {
		tx, err = db.BeginTx(ctx, nil)
		if err == nil {
			break
		}
		if !isBusy(err) || attempt >= 20 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 25 * time.Millisecond):
		}
	}

	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+migrationTable+` (
		version INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return rollback(err)
	}
	for version := range migrations {
		var applied bool
		err := tx.QueryRowContext(ctx,
			"SELECT EXISTS (SELECT 1 FROM "+migrationTable+" WHERE version = ?)", version,
		).Scan(&applied)
		if err != nil {
			return rollback(fmt.Errorf("check migration %d: %w", version, err))
		}
		if applied {
			continue
		}
		if err := applyMigration(ctx, tx, version); err != nil {
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

func applyMigration(ctx context.Context, tx *sql.Tx, version int) error {
	switch version {
	case 0:
		_, err := tx.ExecContext(ctx, migrations[version])
		return err
	case 1:
		for _, column := range []struct {
			name string
			ddl  string
		}{
			{"refresh_status", "ALTER TABLE sources ADD COLUMN refresh_status TEXT NOT NULL DEFAULT 'unknown'"},
			{"refresh_error", "ALTER TABLE sources ADD COLUMN refresh_error TEXT"},
			{"refreshed_at", "ALTER TABLE sources ADD COLUMN refreshed_at INTEGER NOT NULL DEFAULT 0"},
		} {
			if err := ensureColumn(ctx, tx, "sources", column.name, column.ddl); err != nil {
				return err
			}
		}
		return nil
	case 2:
		if err := ensureColumn(ctx, tx, "resource_revisions", "created_at", "ALTER TABLE resource_revisions ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_resource_revisions_created
			ON resource_revisions(marketplace_id, source_id, kind, name, created_at)`)
		return err
	case 3:
		// Existing rows keep their data and take the empty default, which
		// round-trips to an omitted `description` in API responses.
		return ensureColumn(ctx, tx, "resource_revisions", "description", "ALTER TABLE resource_revisions ADD COLUMN description TEXT NOT NULL DEFAULT ''")
	default:
		_, err := tx.ExecContext(ctx, migrations[version])
		return err
	}
}

func ensureColumn(ctx context.Context, tx *sql.Tx, table, column, ddl string) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var found bool
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = tx.ExecContext(ctx, ddl)
	return err
}

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "busy") || strings.Contains(message, "locked")
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
    listed INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
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
    occurred_at INTEGER NOT NULL,
    PRIMARY KEY (source_id, revision_commit)
);

CREATE INDEX IF NOT EXISTS idx_source_occurrences_source
    ON source_occurrences(source_id, occurred_at);

CREATE TABLE IF NOT EXISTS collection_revisions (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    members TEXT NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_collection_revisions_source
    ON collection_revisions(source_id);

CREATE TABLE IF NOT EXISTS revocations (
    kind TEXT NOT NULL,
    target_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    applied_at INTEGER NOT NULL,
    PRIMARY KEY (kind, target_id)
);

CREATE INDEX IF NOT EXISTS idx_revocations_kind_target
    ON revocations(kind, target_id);

CREATE TABLE IF NOT EXISTS jobs (
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_idempotency
    ON jobs(kind, idempotency_key)
    WHERE idempotency_key <> '';

CREATE INDEX IF NOT EXISTS idx_jobs_lease
    ON jobs(status, lease_until, kind, attempt, max_attempts, created_at);
`

const migration1 = `
ALTER TABLE sources ADD COLUMN refresh_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE sources ADD COLUMN refresh_error TEXT;
ALTER TABLE sources ADD COLUMN refreshed_at INTEGER NOT NULL DEFAULT 0;
`

const migration2 = `
ALTER TABLE resource_revisions ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_resource_revisions_created
    ON resource_revisions(marketplace_id, source_id, kind, name, created_at);
`

const migration3 = `
ALTER TABLE resource_revisions ADD COLUMN description TEXT NOT NULL DEFAULT '';
`
