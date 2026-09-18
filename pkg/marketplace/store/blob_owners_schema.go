package store

import "fmt"

const BlobOwnershipSchema = `
CREATE TABLE blob_owner_coverage (version INTEGER PRIMARY KEY);
INSERT INTO blob_owner_coverage (version) VALUES (1);
CREATE TABLE blob_owner_sources (
 source_kind TEXT NOT NULL, registry_id TEXT NOT NULL, source_id TEXT NOT NULL,
 payload_digest TEXT NOT NULL, complete INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY (source_kind, registry_id, source_id)
);
CREATE INDEX blob_owner_sources_pending ON blob_owner_sources(complete, source_kind, registry_id, source_id);
CREATE TABLE blob_resource_owners (
 blob_kind TEXT NOT NULL, blob_digest TEXT NOT NULL, marketplace_id TEXT NOT NULL,
 registry_id TEXT NOT NULL, resource_kind TEXT NOT NULL, name TEXT NOT NULL, content_digest TEXT NOT NULL,
 PRIMARY KEY (blob_kind, blob_digest, marketplace_id, registry_id, resource_kind, name, content_digest)
);
CREATE INDEX blob_resource_owners_digest ON blob_resource_owners(blob_digest, marketplace_id, registry_id, resource_kind, name, content_digest);
CREATE TABLE blob_registry_owners (
 blob_kind TEXT NOT NULL, blob_digest TEXT NOT NULL, registry_id TEXT NOT NULL,
 PRIMARY KEY (blob_kind, blob_digest, registry_id)
);
CREATE INDEX blob_registry_owners_digest ON blob_registry_owners(blob_digest, registry_id);
CREATE INDEX blob_registry_occurrences_manifest ON registry_occurrences(registry_id, manifest_digest);
CREATE INDEX blob_registry_publications_manifest ON registry_publications(registry_id, manifest_digest);
CREATE INDEX blob_resource_occurrences_manifest ON resource_occurrences(registry_id, manifest_digest, marketplace_id, kind, name, content_digest);
CREATE INDEX blob_publication_resources_manifest ON publication_resources(registry_id, manifest_digest, marketplace_id, kind, name, content_digest);
`

func BlobOwnershipMigration(postgres bool) string {
	schema := BlobOwnershipSchema + `
INSERT INTO blob_resource_owners (blob_kind,blob_digest,marketplace_id,registry_id,resource_kind,name,content_digest)
SELECT 'revision',content_digest,marketplace_id,registry_id,kind,name,content_digest FROM resource_revisions WHERE 1=1 ON CONFLICT DO NOTHING;
INSERT INTO blob_resource_owners (blob_kind,blob_digest,marketplace_id,registry_id,resource_kind,name,content_digest)
SELECT 'archive',archive_digest,marketplace_id,registry_id,kind,name,content_digest FROM resource_revisions WHERE archive_digest IS NOT NULL AND archive_digest<>'' ON CONFLICT DO NOTHING;
`
	sources := []struct{ table, kind, id, payload, condition string }{
		{"resource_assessments", "inspection", "assessment_digest", "inspection_manifest_digest", "1=1"},
		{"registry_occurrences", "registry", "manifest_digest", "manifest_digest", "1=1"},
		{"registry_publications", "registry", "manifest_digest", "manifest_digest", "1=1"},
		{"resource_occurrences", "registry", "manifest_digest", "manifest_digest", "1=1"},
		{"publication_resources", "registry", "manifest_digest", "manifest_digest", "1=1"},
		{"collection_revisions", "collection", "id", "manifest_digest", "1=1"},
		{"resource_revisions", "collection", "content_digest", "content_digest", "kind='collection'"},
	}
	for _, source := range sources {
		schema += fmt.Sprintf(`INSERT INTO blob_owner_sources (source_kind,registry_id,source_id,payload_digest,complete)
SELECT '%s',registry_id,%s,%s,0 FROM %s WHERE %s ON CONFLICT(source_kind,registry_id,source_id) DO NOTHING;
`, source.kind, source.id, source.payload, source.table, source.condition)
		condition := "1=1"
		if source.table == "resource_revisions" {
			condition = "NEW.kind='collection'"
		}
		body := fmt.Sprintf(`INSERT INTO blob_owner_sources (source_kind,registry_id,source_id,payload_digest,complete)
SELECT '%s',NEW.registry_id,NEW.%s,NEW.%s,0 WHERE %s
ON CONFLICT(source_kind,registry_id,source_id) DO UPDATE SET complete=0;`, source.kind, source.id, source.payload, condition)
		if source.table == "resource_revisions" {
			body += `
INSERT INTO blob_resource_owners (blob_kind,blob_digest,marketplace_id,registry_id,resource_kind,name,content_digest)
VALUES ('revision',NEW.content_digest,NEW.marketplace_id,NEW.registry_id,NEW.kind,NEW.name,NEW.content_digest) ON CONFLICT DO NOTHING;
INSERT INTO blob_resource_owners (blob_kind,blob_digest,marketplace_id,registry_id,resource_kind,name,content_digest)
SELECT 'archive',NEW.archive_digest,NEW.marketplace_id,NEW.registry_id,NEW.kind,NEW.name,NEW.content_digest WHERE NEW.archive_digest IS NOT NULL AND NEW.archive_digest<>'' ON CONFLICT DO NOTHING;
`
		}
		name := "blob_owner_track_" + source.table
		if postgres {
			schema += fmt.Sprintf("CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN %s RETURN NEW; END $$;\nCREATE TRIGGER %s AFTER INSERT ON %s FOR EACH ROW EXECUTE FUNCTION %s();\n", name, body, name, source.table, name)
		} else {
			schema += fmt.Sprintf("CREATE TRIGGER %s AFTER INSERT ON %s BEGIN %s END;\n", name, source.table, body)
		}
	}
	return schema
}
