package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/jackc/pgx/v5"
)

type recordingQueryer struct {
	queryer
	texts []string
}

func (r *recordingQueryer) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.texts = append(r.texts, query)
	return r.queryer.QueryContext(ctx, query, args...)
}

func (r *recordingQueryer) reset() {
	r.texts = nil
}

func (r *recordingQueryer) anyContaining(markers ...string) bool {
	for _, text := range r.texts {
		for _, marker := range markers {
			if strings.Contains(text, marker) {
				return true
			}
		}
	}
	return false
}

func internalTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN is required for the PostgreSQL backend")
	}
	schema := fmt.Sprintf("am_t_%d", time.Now().UnixNano())
	s := NewWithSchema(dsn, schema)
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	t.Cleanup(func() {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return
		}
		defer func() { _ = db.Close() }()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	})
	return s
}

func seedConditionalCatalog(t *testing.T, ctx context.Context, s *Store) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Catalog", CanonicalURL: "https://marketplace.test"}))
	must(s.PutRegistry(ctx, store.Registry{ID: "reg", Slug: "reg", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}))
	manifest := store.RevisionDigest("sha256:" + strings.Repeat("f", 64))
	must(s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: "reg", Ref: "main", Commit: "head", RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror}))
	for _, seeded := range []struct{ kind, name, digestChar string }{
		{"skill", "one", "a"},
		{"collection", "bundle", "e"},
	} {
		coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: seeded.kind, Name: seeded.name}
		rev := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat(seeded.digestChar, 64)), License: "MIT"}
		must(s.PutResourceRevision(ctx, rev))
		must(s.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "head", ManifestDigest: manifest}))
		assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
			Coordinate: coord, ContentDigest: rev.ContentDigest, AnalyzerVersion: "catalog/v1",
			PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)), Decision: store.AssessmentDecisionAllow,
			InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("e", 64)), AssessedAt: time.Unix(1, 0).UTC(),
		})
		must(err)
		must(s.PutResourceAssessment(ctx, assessment))
		must(s.SetCurrentResourceAssessment(ctx, assessment))
		if seeded.kind == "collection" {
			must(s.PutCollectionRevision(ctx, store.CollectionRevision{
				ID: rev.ContentDigest.String(), RegistryID: "reg", ManifestDigest: rev.ContentDigest,
				Members: []store.ResourceCoordinate{{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: "one"}},
			}))
		}
	}
	must(s.SetRegistryHead(ctx, "reg", "head", ""))
}

func TestConditionalPageReaderIntegrityOnly(t *testing.T) {
	ctx := context.Background()
	s := internalTestStore(t)
	seedConditionalCatalog(t, ctx, s)

	if !s.OwnsCatalogProjectionStore(s) {
		t.Fatal("root store does not confirm ownership of itself")
	}
	if s.OwnsCatalogProjectionStore(nil) {
		t.Fatal("root store claimed ownership of a foreign store")
	}

	req := store.CatalogProjectionPageRequest{Marketplace: "mp", Limit: 10}
	err := s.WithTx(ctx, func(txCtx context.Context, meta store.MetadataStore) error {
		txStore, ok := meta.(*Store)
		if !ok {
			t.Fatalf("transaction store = %T", meta)
		}
		if txStore.OwnsCatalogProjectionStore(txStore) {
			t.Fatal("borrowed transaction claimed conditional ownership")
		}
		recorder := &recordingQueryer{queryer: txStore.q}
		txStore.q = recorder

		page, unchanged, err := txStore.readCatalogProjectionPageConditional(txCtx, req, func(store.CatalogVersion) bool { return true })
		if err != nil {
			return err
		}
		if !unchanged || len(page.Resources) != 0 || page.CatalogVersion == 0 {
			t.Fatalf("conditional read = (%v, %+v)", unchanged, page)
		}
		if !recorder.anyContaining("page_keys") || !recorder.anyContaining("c.includes") {
			t.Fatal("matching validator did not run the integrity query")
		}
		if recorder.anyContaining("rr.archive_digest", "WITH requested", "manifest_digest, members, includes") {
			t.Fatal("matching validator still ran the wide decorated read or hydration")
		}

		recorder.reset()
		page, unchanged, err = txStore.readCatalogProjectionPageConditional(txCtx, req, func(store.CatalogVersion) bool { return false })
		if err != nil {
			return err
		}
		if unchanged || len(page.Resources) != 2 {
			t.Fatalf("non-matching read = (%v, %d resources)", unchanged, len(page.Resources))
		}
		if !recorder.anyContaining("rr.archive_digest") {
			t.Fatal("non-matching validator did not run the wide page read")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConditionalPageReaderPanicReleasesOwnedTransaction(t *testing.T) {
	s := internalTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	beforeInUse := s.db.Stats().InUse
	if beforeInUse != 0 {
		t.Fatalf("pool connections in use before read = %d, want 0", beforeInUse)
	}
	recovered := false
	func() {
		defer func() {
			if r := recover(); r == "controlled callback panic" {
				recovered = true
			}
		}()
		_, _, _ = s.ReadCatalogProjectionPageConditional(ctx, store.CatalogProjectionPageRequest{Marketplace: "mp", Limit: 1}, func(store.CatalogVersion) bool {
			panic("controlled callback panic")
		})
	}()
	if !recovered {
		t.Fatal("controlled callback panic was not recovered")
	}
	if inUse := s.db.Stats().InUse; inUse != beforeInUse {
		t.Fatalf("pool connections in use after recovered panic = %d, want %d", inUse, beforeInUse)
	}
}

func TestConditionalPageReaderBorrowedTransactionKeepsOrdinaryRead(t *testing.T) {
	ctx := context.Background()
	s := internalTestStore(t)
	seedConditionalCatalog(t, ctx, s)

	req := store.CatalogProjectionPageRequest{Marketplace: "mp", Limit: 10}
	err := s.WithTx(ctx, func(txCtx context.Context, meta store.MetadataStore) error {
		matcherInvoked := false
		page, unchanged, err := meta.(store.ConditionalCatalogProjectionStore).ReadCatalogProjectionPageConditional(txCtx, req, func(store.CatalogVersion) bool {
			matcherInvoked = true
			return true
		})
		if err != nil {
			return err
		}
		if unchanged || matcherInvoked || len(page.Resources) != 2 {
			t.Fatalf("borrowed transaction conditional = (%v, matcher=%v, %d resources), want ordinary page", unchanged, matcherInvoked, len(page.Resources))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
