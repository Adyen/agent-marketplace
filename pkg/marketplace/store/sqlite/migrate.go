package sqlite

// migrations contains the complete schema for the SQLite metadata adapter.
// Additional migrations can be appended to this slice over time.
var migrations = []string{migration0, migration1}

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
