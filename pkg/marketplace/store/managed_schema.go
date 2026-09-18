package store

const ManagedConfigurationSchema = `
CREATE TABLE managed_configuration_heads (
    marketplace_id TEXT PRIMARY KEY REFERENCES marketplaces(id),
    revision BIGINT NOT NULL CHECK (revision >= 0)
);
CREATE TABLE managed_configuration_changes (
    marketplace_id TEXT NOT NULL REFERENCES managed_configuration_heads(marketplace_id),
    revision BIGINT NOT NULL CHECK (revision > 0),
    parent_revision BIGINT NOT NULL CHECK (parent_revision >= 0),
    actor_issuer TEXT NOT NULL,
    actor_subject TEXT NOT NULL,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_digest TEXT NOT NULL,
    record TEXT NOT NULL,
    PRIMARY KEY (marketplace_id, revision),
    UNIQUE (marketplace_id, actor_issuer, actor_subject, operation, idempotency_key)
);
`
