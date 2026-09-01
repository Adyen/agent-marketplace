package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

const testDescription = "Analyze user retention and engagement patterns by cohort."

func seedRevision(t *testing.T, s *sqlite.Store, name, description string) store.ResourceRevision {
	t.Helper()
	ctx := context.Background()
	rev := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: name},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
		ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("b", 64)),
		Description:   description,
		License:       "MIT",
	}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("PutResourceRevision: %v", err)
	}
	manifest := store.RevisionDigest("sha256:" + strings.Repeat("c", 64))
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: rev.Coordinate.Registry, Ref: "main", Commit: "commit", RegistryRevisionManifestDigest: manifest,
		Acquisition: store.AcquisitionMirror,
	}); err != nil {
		t.Fatalf("PutRegistryOccurrence: %v", err)
	}
	if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, Commit: "commit", ManifestDigest: manifest,
	}); err != nil {
		t.Fatalf("PutResourceOccurrence: %v", err)
	}
	assessment := store.ResourceAssessment{
		Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, AnalyzerVersion: "analyzer/v1",
		PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("d", 64)), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("e", 64)), AssessedAt: time.Now().UTC(),
	}
	if err := s.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatalf("PutResourceAssessment: %v", err)
	}
	if err := s.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatalf("SetCurrentResourceAssessment: %v", err)
	}
	return rev
}

func openSeededStore(t *testing.T) *sqlite.Store {
	t.Helper()
	ctx := context.Background()
	s := sqlite.New(filepath.Join(t.TempDir(), "marketplace.db"))
	if err := s.Open(ctx); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Marketplace"}); err != nil {
		t.Fatalf("PutMarketplace: %v", err)
	}
	if err := s.PutRegistry(ctx, store.Registry{ID: "reg", Slug: "reg", Listed: true, Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatalf("PutRegistry: %v", err)
	}
	return s
}

func TestDescriptionRoundTripsThroughEveryReadPath(t *testing.T) {
	ctx := context.Background()
	s := openSeededStore(t)
	rev := seedRevision(t, s, "review", testDescription)

	t.Run("GetResourceRevision", func(t *testing.T) {
		got, err := s.GetResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
		if err != nil {
			t.Fatal(err)
		}
		assertDescription(t, got.Description)
	})
	t.Run("GetLatestResourceRevision", func(t *testing.T) {
		got, err := s.GetLatestResourceRevision(ctx, rev.Coordinate)
		if err != nil {
			t.Fatal(err)
		}
		assertDescription(t, got.Description)
	})
	for name, read := range map[string]func() ([]store.AssessedResourceRevision, error){
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
			got, err := read()
			if err != nil {
				t.Fatal(err)
			}
			assertSingleDescription(t, got)
		})
	}
}

func assertDescription(t *testing.T, got string) {
	t.Helper()
	if got != testDescription {
		t.Fatalf("description = %q, want %q", got, testDescription)
	}
}

func assertSingleDescription(t *testing.T, revs []store.AssessedResourceRevision) {
	t.Helper()
	if len(revs) != 1 {
		t.Fatalf("got %d revisions, want 1", len(revs))
	}
	assertDescription(t, revs[0].Description)
}

func TestPutResourceRevisionBoundsDescription(t *testing.T) {
	ctx := context.Background()
	s := openSeededStore(t)
	hostile := "multi\nline\x1b[31m" + strings.Repeat("x", store.MaxDescriptionBytes*3)
	rev := seedRevision(t, s, "hostile", hostile)
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

func TestLegacySchemaFailsClearly(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE marketplace_schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	err = sqlite.New(path).Open(ctx)
	if err == nil || !strings.Contains(err.Error(), "incompatible legacy SQLite metadata schema") {
		t.Fatalf("Open legacy schema error = %v, want clear incompatibility error", err)
	}
}
