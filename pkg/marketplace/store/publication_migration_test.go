package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func TestPublicationMigrationPreservesLegacy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	meta := sqlite.New(path)
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "managed-test", Name: "Legacy", CanonicalURL: "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutRegistry(ctx, store.Registry{ID: "registry", Slug: "registry", Ref: "main", Listed: true, Acquisition: store.AcquisitionIndexOnly}); err != nil {
		t.Fatal(err)
	}
	legacy := publicationRequest(t, "legacy", "old")
	if err := meta.PutRegistryOccurrence(ctx, legacy.Occurrence); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceRevision(ctx, legacy.Resources[0]); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceOccurrence(ctx, legacy.ResourceOccurrences[0]); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceAssessment(ctx, legacy.Assessments[0]); err != nil {
		t.Fatal(err)
	}
	if err := meta.SetCurrentResourceAssessment(ctx, legacy.Assessments[0]); err != nil {
		t.Fatal(err)
	}
	before, err := meta.GetResourceRevision(ctx, legacy.Resources[0].Coordinate, legacy.Resources[0].ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	occurrence, err := meta.GetRegistryOccurrence(ctx, "registry", legacy.Occurrence.Commit)
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	removeBlobOwnershipMigration(t, db)
	for _, statement := range []string{
		`DROP VIEW current_resource_occurrences`, `DROP VIEW effective_registry_heads`, `DROP VIEW all_resource_occurrences`, `DROP VIEW all_registry_occurrences`,
		`DROP TABLE current_registry_publications`, `DROP TABLE publication_resources`, `DROP TABLE registry_publications`,
		`DELETE FROM marketplace_metadata_v2_migrations WHERE version=4`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := meta.GetResourceRevision(ctx, legacy.Resources[0].Coordinate, legacy.Resources[0].ContentDigest)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("migration rewrote exact legacy revision: %+v %v", after, err)
	}
	next := publicationRequest(t, "new-roots", "new")
	if err := meta.PublishRegistryPublication(ctx, next); err != nil {
		t.Fatal(err)
	}
	after, err = meta.GetResourceRevision(ctx, legacy.Resources[0].Coordinate, legacy.Resources[0].ContentDigest)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("publication rewrote legacy revision: %+v %v", after, err)
	}
	got, err := meta.GetRegistryOccurrence(ctx, "registry", legacy.Occurrence.Commit)
	if err != nil || !reflect.DeepEqual(occurrence, got) {
		t.Fatalf("legacy commit occurrence changed: %+v %v", got, err)
	}
	page, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: "managed-test", Limit: 10})
	if err != nil || len(page.Resources) != 1 || page.Resources[0].Coordinate.Name != "new" {
		t.Fatalf("legacy membership leaked: %+v %v", page, err)
	}
}
