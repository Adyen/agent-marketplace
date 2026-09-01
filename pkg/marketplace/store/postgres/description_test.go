package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/jackc/pgx/v5"
)

const testDescription = "Analyze user retention and engagement patterns by cohort."

func digestOf(c string) string { return "sha256:" + strings.Repeat(c, 64) }

func seedDescriptionFixture(t *testing.T, s *postgres.Store, name, description string) store.ResourceRevision {
	t.Helper()
	ctx := context.Background()

	if err := s.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Marketplace"}); err != nil {
		t.Fatalf("PutMarketplace: %v", err)
	}
	if err := s.PutRegistry(ctx, store.Registry{
		ID: "reg", Slug: "reg", Ref: "main", Listed: true, Acquisition: store.AcquisitionMirror,
	}); err != nil {
		t.Fatalf("PutRegistry: %v", err)
	}

	rev := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: name},
		ContentDigest: store.RevisionDigest(digestOf("a")),
		ArchiveDigest: store.ArchiveDigest(digestOf("b")),
		Description:   description,
		License:       "MIT",
	}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("PutResourceRevision: %v", err)
	}
	manifest := store.RevisionDigest(digestOf("c"))
	if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, Commit: "commit-1", ManifestDigest: manifest,
	}); err != nil {
		t.Fatalf("PutResourceOccurrence: %v", err)
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, AnalyzerVersion: "analyzer/v1",
		PolicyDigest: store.PolicyDigest(digestOf("d")), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest(digestOf("e")), AssessedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("FinalizeResourceAssessment: %v", err)
	}
	if err := s.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatalf("PutResourceAssessment: %v", err)
	}
	if err := s.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatalf("SetCurrentResourceAssessment: %v", err)
	}
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: rev.Coordinate.Registry, Ref: "main", Commit: "commit-1",
		RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror,
	}); err != nil {
		t.Fatalf("PutRegistryOccurrence: %v", err)
	}
	return rev
}

// Mirrors the SQLite coverage: every read path must project the description.
func TestDescriptionRoundTripsThroughEveryReadPath(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	rev := seedDescriptionFixture(t, s, "review", testDescription)

	assert := func(t *testing.T, got string) {
		t.Helper()
		if got != testDescription {
			t.Fatalf("description = %q, want %q", got, testDescription)
		}
	}
	t.Run("GetResourceRevision", func(t *testing.T) {
		got, err := s.GetResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
		if err != nil {
			t.Fatal(err)
		}
		assert(t, got.Description)
	})
	t.Run("GetLatestResourceRevision", func(t *testing.T) {
		got, err := s.GetLatestResourceRevision(ctx, rev.Coordinate)
		if err != nil {
			t.Fatal(err)
		}
		assert(t, got.Description)
	})

	for name, list := range map[string]func() ([]store.AssessedResourceRevision, error){
		"ListResourceRevisions": func() ([]store.AssessedResourceRevision, error) {
			return s.ListResourceRevisions(ctx, rev.Coordinate)
		},
		"ListResourceRevisionsByArchiveDigest": func() ([]store.AssessedResourceRevision, error) {
			return s.ListResourceRevisionsByArchiveDigest(ctx, "mp", rev.ArchiveDigest)
		},
		"ListResourceRevisionsByContentDigest": func() ([]store.AssessedResourceRevision, error) {
			return s.ListResourceRevisionsByContentDigest(ctx, "mp", rev.ContentDigest)
		},
		"SearchResourceRevisions": func() ([]store.AssessedResourceRevision, error) {
			return s.SearchResourceRevisions(ctx, store.ResourceSearchFilter{Marketplace: "mp", Query: "review"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			revs, err := list()
			if err != nil {
				t.Fatal(err)
			}
			if len(revs) != 1 {
				t.Fatalf("got %d revisions, want 1", len(revs))
			}
			assert(t, revs[0].Description)
		})
	}
}

func TestPutResourceRevisionBoundsDescription(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	hostile := "multi\nline\x1b[31m" + strings.Repeat("x", store.MaxDescriptionBytes*3)
	rev := seedDescriptionFixture(t, s, "hostile", hostile)

	got, err := s.GetResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Description) > store.MaxDescriptionBytes {
		t.Fatalf("stored description length = %d, want <= %d", len(got.Description), store.MaxDescriptionBytes)
	}
	if strings.ContainsAny(got.Description, "\n\x1b") {
		t.Fatal("stored description retained control characters")
	}
}

func TestPutResourceRevisionConflictsInsteadOfUpdatingDescription(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	rev := seedDescriptionFixture(t, s, "review", "original description")

	rev.Description = testDescription
	if err := s.PutResourceRevision(ctx, rev); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("re-put error = %v, want ErrConflict", err)
	}
	got, err := s.GetResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "original description" {
		t.Fatalf("immutable description changed to %q", got.Description)
	}
}

func TestLegacyMigrationEpochFailsClearly(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	schema := testSchema(t)
	t.Cleanup(func() { dropSchema(t, dsn, schema) })
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	quoted := pgx.Identifier{schema}.Sanitize()
	for _, statement := range []string{
		"CREATE SCHEMA " + quoted,
		`CREATE TABLE ` + quoted + `.marketplace_schema_migrations (version INTEGER PRIMARY KEY, applied_at BIGINT NOT NULL)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	s := postgres.NewWithSchema(dsn, schema)
	err = s.Open(ctx)
	if err == nil || !strings.Contains(err.Error(), "legacy PostgreSQL metadata schema") {
		t.Fatalf("Open error = %v, want clear legacy schema error", err)
	}
}
