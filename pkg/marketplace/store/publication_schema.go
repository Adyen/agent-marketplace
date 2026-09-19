package store

const PublicationSchema = `
CREATE TABLE registry_publications (
 registry_id TEXT NOT NULL, publication_id TEXT NOT NULL, record TEXT NOT NULL,
 ref TEXT NOT NULL, roots TEXT NOT NULL, revision_commit TEXT NOT NULL,
 manifest_digest TEXT NOT NULL, acquisition TEXT NOT NULL,
 mirrored_artifact_digest TEXT, source_origin TEXT, occurred_at BIGINT NOT NULL,
 PRIMARY KEY (registry_id, publication_id)
);
CREATE TABLE publication_resources (
 registry_id TEXT NOT NULL, publication_id TEXT NOT NULL,
 marketplace_id TEXT NOT NULL, kind TEXT NOT NULL, name TEXT NOT NULL,
 content_digest TEXT NOT NULL, revision_commit TEXT NOT NULL, manifest_digest TEXT NOT NULL, source_path TEXT,
 PRIMARY KEY (registry_id, publication_id, marketplace_id, kind, name),
 FOREIGN KEY (registry_id, publication_id) REFERENCES registry_publications(registry_id, publication_id)
);
CREATE INDEX publication_resources_exact ON publication_resources(marketplace_id, registry_id, kind, name, content_digest);
CREATE INDEX registry_publications_history ON registry_publications(registry_id, occurred_at);
CREATE TABLE current_registry_publications (
 registry_id TEXT PRIMARY KEY, publication_id TEXT NOT NULL,
 FOREIGN KEY (registry_id, publication_id) REFERENCES registry_publications(registry_id, publication_id)
);
CREATE VIEW all_registry_occurrences AS
 SELECT registry_id, ref, roots, revision_commit, manifest_digest, acquisition,
 mirrored_artifact_digest, source_origin, occurred_at, 'legacy:' || revision_commit AS occurrence_key FROM registry_occurrences
 UNION ALL
 SELECT registry_id, ref, roots, revision_commit, manifest_digest, acquisition,
 mirrored_artifact_digest, source_origin, occurred_at, 'publication:' || publication_id AS occurrence_key FROM registry_publications;
CREATE VIEW all_resource_occurrences AS
 SELECT marketplace_id, registry_id, kind, name, content_digest, revision_commit, manifest_digest, source_path,
 'legacy:' || revision_commit AS occurrence_key FROM resource_occurrences
 UNION ALL
 SELECT marketplace_id, registry_id, kind, name, content_digest, revision_commit, manifest_digest, source_path,
 'publication:' || publication_id AS occurrence_key FROM publication_resources;
CREATE VIEW current_resource_occurrences AS
 SELECT r.marketplace_id, r.registry_id, r.kind, r.name, r.content_digest, r.revision_commit, r.manifest_digest, r.source_path,
 'legacy:' || r.revision_commit AS occurrence_key, so.source_origin
 FROM resource_occurrences r JOIN registry_heads h ON h.registry_id=r.registry_id AND h.revision_commit=r.revision_commit
 JOIN registry_occurrences so ON so.registry_id=r.registry_id AND so.revision_commit=r.revision_commit
 WHERE NOT EXISTS (SELECT 1 FROM current_registry_publications p WHERE p.registry_id=r.registry_id)
 UNION ALL
 SELECT r.marketplace_id, r.registry_id, r.kind, r.name, r.content_digest, r.revision_commit, r.manifest_digest, r.source_path,
 'publication:' || r.publication_id AS occurrence_key, so.source_origin
 FROM publication_resources r JOIN current_registry_publications h ON h.registry_id=r.registry_id AND h.publication_id=r.publication_id
 JOIN registry_publications so ON so.registry_id=r.registry_id AND so.publication_id=r.publication_id;
CREATE VIEW effective_registry_heads AS
 SELECT h.registry_id, h.revision_commit, 'legacy:' || h.revision_commit AS occurrence_key FROM registry_heads h
 WHERE NOT EXISTS (SELECT 1 FROM current_registry_publications p WHERE p.registry_id=h.registry_id)
 UNION ALL
 SELECT p.registry_id, p.revision_commit, 'publication:' || p.publication_id AS occurrence_key
 FROM registry_publications p JOIN current_registry_publications h ON h.registry_id=p.registry_id AND h.publication_id=p.publication_id;
`
