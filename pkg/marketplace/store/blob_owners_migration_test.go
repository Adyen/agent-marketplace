package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func removeBlobOwnershipMigration(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range []string{"resource_assessments", "registry_occurrences", "registry_publications", "resource_occurrences", "publication_resources", "collection_revisions", "resource_revisions"} {
		if _, err := db.Exec(`DROP TRIGGER blob_owner_track_` + table); err != nil {
			t.Fatal(err)
		}
	}
	for _, index := range []string{"blob_registry_occurrences_manifest", "blob_registry_publications_manifest", "blob_resource_occurrences_manifest", "blob_publication_resources_manifest"} {
		if _, err := db.Exec(`DROP INDEX ` + index); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"blob_resource_owners", "blob_registry_owners", "blob_owner_sources", "blob_owner_coverage"} {
		if _, err := db.Exec(`DROP TABLE ` + table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`DELETE FROM marketplace_metadata_v2_migrations WHERE version=5`); err != nil {
		t.Fatal(err)
	}
}

func TestBlobOwnersMigrationAndMissingCoverage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "metadata.db")
	meta := sqlite.New(path)
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "managed-test", Name: "Legacy", CanonicalURL: "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	putBlobOwnershipRegistry(t, meta, "registry")
	req, digest := blobOwnershipPublication(t, "registry", "", "# Pre-migration body")
	if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
		t.Fatal(err)
	}
	if owners, _, err := meta.ListBlobOwners(ctx, "managed-test", "content", digest); err != nil || len(owners) != 1 {
		t.Fatalf("seed control: %+v %v", owners, err)
	}
	if err := meta.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	removeBlobOwnershipMigration(t, db)
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := meta.ListBlobOwners(ctx, "managed-test", "content", digest); !errors.Is(err, store.ErrBlobOwnersIncomplete) {
		t.Fatalf("migration assumed legacy coverage: %v", err)
	}
	result, err := store.BackfillBlobOwners(ctx, meta, blobs)
	if err != nil || result.Processed != 2 || result.Remaining {
		t.Fatalf("legacy migration backfill: %+v %v", result, err)
	}
	if owners, _, err := meta.ListBlobOwners(ctx, "managed-test", "content", digest); err != nil || len(owners) != 1 {
		t.Fatalf("legacy owners lost: %+v %v", owners, err)
	}
	if _, err := db.Exec(`DELETE FROM blob_owner_coverage`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := meta.ListBlobOwners(ctx, "managed-test", "content", digest); !errors.Is(err, store.ErrBlobOwnersIncomplete) {
		t.Fatalf("missing coverage marker accepted: %v", err)
	}
}

type countingBlobOwnerMetadata struct {
	store.MetadataStore
	reads int
}

func (s *countingBlobOwnerMetadata) GetResourceRevision(ctx context.Context, coordinate store.ResourceCoordinate, digest store.RevisionDigest) (store.AssessedResourceRevision, error) {
	s.reads++
	return s.MetadataStore.GetResourceRevision(ctx, coordinate, digest)
}

func TestBlobOwnersReadLimitFailsClosedBeforeOwnerExpansion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "metadata.db")
	meta := sqlite.New(path)
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "managed-test", Name: "Bounded", CanonicalURL: "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	putBlobOwnershipRegistry(t, meta, "registry")
	req, digest := blobOwnershipPublication(t, "registry", "", "# Known owner")
	if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	counting := &countingBlobOwnerMetadata{MetadataStore: meta}
	if owners, _, err := store.ReadBlobOwners(ctx, db, counting, "managed-test", "content", digest); err != nil || len(owners) != 1 || counting.reads != 1 {
		t.Fatalf("positive owner expansion: %+v reads=%d %v", owners, counting.reads, err)
	}
	for i := 0; i < store.BlobOwnerReadLimit; i++ {
		if _, err := db.Exec(`INSERT INTO blob_resource_owners (blob_kind,blob_digest,marketplace_id,registry_id,resource_kind,name,content_digest) VALUES ('content',?,'managed-test','registry','skill',?,?)`, digest, fmt.Sprintf("overflow-%d", i), req.Resources[0].ContentDigest); err != nil {
			t.Fatal(err)
		}
	}
	counting.reads = 0
	if _, _, err := store.ReadBlobOwners(ctx, db, counting, "managed-test", "content", digest); !errors.Is(err, store.ErrBlobOwnersIncomplete) || counting.reads != 0 {
		t.Fatalf("truncated/unbounded owner expansion: reads=%d %v", counting.reads, err)
	}
}

func TestBlobOwnerSQLFailureRollsBackPublication(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "metadata.db")
	meta := sqlite.New(path)
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "managed-test", Name: "Rollback", CanonicalURL: "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	putBlobOwnershipRegistry(t, meta, "registry")
	req, digest := blobOwnershipPublication(t, "registry", "first", "# Baseline")
	if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
		t.Fatal(err)
	}
	before, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: "managed-test", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TRIGGER reject_blob_owner BEFORE INSERT ON blob_resource_owners BEGIN SELECT RAISE(ABORT,'ownership index unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	next, newDigest := blobOwnershipPublication(t, "registry", "second", "# New body")
	if err := store.PublishRegistryRevision(ctx, meta, blobs, next); err == nil {
		t.Fatal("publication succeeded without persisted ownership")
	}
	current, err := meta.CurrentRegistryPublication(ctx, "registry")
	if err != nil || current.ID != "first" {
		t.Fatalf("failed index changed pointer: %+v %v", current, err)
	}
	after, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: "managed-test", Limit: 10})
	if err != nil || after.CatalogVersion != before.CatalogVersion || len(after.Resources) != 1 || after.Resources[0].Assessment.AssessmentDigest != before.Resources[0].Assessment.AssessmentDigest {
		t.Fatalf("failed index changed assessment/catalog: %+v %v", after, err)
	}
	if owners, _, err := meta.ListBlobOwners(ctx, "managed-test", "content", digest); err != nil || len(owners) != 1 {
		t.Fatalf("failed index poisoned existing coverage: %+v %v", owners, err)
	}
	if _, _, err := meta.ListBlobOwners(ctx, "managed-test", "content", newDigest); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed index exposed new body: %v", err)
	}
}
