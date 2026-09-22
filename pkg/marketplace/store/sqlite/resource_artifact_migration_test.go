package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestResourceArtifactMigrationPreservesLegacyRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE `+migrationTable+` (version INTEGER PRIMARY KEY,applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for version, migration := range migrations[:len(migrations)-1] {
		if _, err := db.ExecContext(ctx, migration); err != nil {
			t.Fatalf("migration %d: %v", version, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO `+migrationTable+` (version,applied_at) VALUES (?,0)`, version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_revisions (marketplace_id,registry_id,kind,name,content_digest,archive_digest) VALUES ('mp','reg','skill','review','legacy-content','legacy-archive')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		s := New(path)
		if err := s.Open(ctx); err != nil {
			t.Fatal(err)
		}
		revision, err := s.getIntrinsicResourceRevision(ctx, store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: "review"}, "legacy-content")
		if err != nil || revision.ArchiveDigest != "legacy-archive" {
			t.Fatalf("legacy row changed: %+v %v", revision, err)
		}
		records, err := s.ListResourceArtifacts(ctx, revision.Coordinate, revision.ContentDigest)
		if err != nil || len(records) != 0 {
			t.Fatalf("migration fabricated representation: %+v %v", records, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
