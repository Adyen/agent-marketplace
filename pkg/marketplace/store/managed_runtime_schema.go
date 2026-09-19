package store

const ManagedRuntimeSchema = `
ALTER TABLE managed_configuration_heads ADD COLUMN attempt_sequence BIGINT NOT NULL DEFAULT 0 CHECK (attempt_sequence >= 0);
CREATE TABLE managed_registry_identities (
 registry_id TEXT PRIMARY KEY, marketplace_id TEXT NOT NULL
);
CREATE TABLE managed_registry_names (
 name TEXT PRIMARY KEY, registry_id TEXT NOT NULL
);
CREATE TABLE managed_refresh_events (
 marketplace_id TEXT NOT NULL, sequence BIGINT NOT NULL CHECK (sequence > 0),
 registry_id TEXT NOT NULL, job_id TEXT NOT NULL, attempt INTEGER NOT NULL CHECK (attempt > 0),
 outcome TEXT NOT NULL CHECK (outcome IN ('started','failed','superseded','succeeded')), record TEXT NOT NULL,
 PRIMARY KEY (marketplace_id, sequence), UNIQUE (job_id, attempt, outcome)
);
CREATE INDEX managed_refresh_events_registry ON managed_refresh_events(marketplace_id, registry_id, sequence);
CREATE INDEX managed_refresh_events_job ON managed_refresh_events(marketplace_id, job_id, sequence);
CREATE TABLE managed_registry_refresh_status (
 marketplace_id TEXT NOT NULL, registry_id TEXT NOT NULL,
 last_attempt_sequence BIGINT NOT NULL, last_success_sequence BIGINT,
 PRIMARY KEY (marketplace_id, registry_id),
 FOREIGN KEY (marketplace_id, last_attempt_sequence) REFERENCES managed_refresh_events(marketplace_id, sequence),
 FOREIGN KEY (marketplace_id, last_success_sequence) REFERENCES managed_refresh_events(marketplace_id, sequence)
);
`
