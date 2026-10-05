package sqlite_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func TestRegistryCatalogResetRollsBackLateStorageFailure(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "metadata.db")
	meta := sqlite.New(path)
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	if err := meta.PutRegistry(ctx, store.Registry{ID: "registry", Slug: "registry", Ref: "main", Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatal(err)
	}
	revision := store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "mp", Registry: "registry", Kind: "skill", Name: "review"}, ContentDigest: "sha256:content", ArchiveDigest: "sha256:original"}
	if err := meta.PutResourceRevision(ctx, revision); err != nil {
		t.Fatal(err)
	}
	collection := store.CollectionRevision{ID: "team", RegistryID: "registry", ManifestDigest: "sha256:collection"}
	if err := meta.PutCollectionRevision(ctx, collection); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER fail_reset BEFORE DELETE ON collection_revisions BEGIN SELECT RAISE(ABORT, 'injected reset failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := meta.ResetRegistryCatalog(ctx, "registry", false); err == nil || !strings.Contains(err.Error(), "injected reset failure") {
		t.Fatalf("reset did not reach injected storage failure: %v", err)
	}
	if _, err := meta.GetCollectionRevision(ctx, collection.ID); err != nil {
		t.Fatalf("rollback lost collection: %v", err)
	}
	revision.License = "Apache-2.0"
	if err := meta.PutResourceRevision(ctx, revision); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("rollback lost original immutable resource: %v", err)
	}
}
