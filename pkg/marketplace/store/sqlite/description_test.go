package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

const testDescription = "Analyze user retention and engagement patterns by cohort."

func seedRevision(t *testing.T, s *sqlite.Store, name, description string) store.ResourceRevision {
	t.Helper()
	ctx := context.Background()

	rev := store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: "mp", Source: "src", Kind: "skill", Name: name,
		},
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
		ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("b", 64)),
		Description:   description,
		License:       "MIT",
	}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("PutResourceRevision: %v", err)
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
	if err := s.PutSource(ctx, store.Source{ID: "src", Slug: "src", Listed: true, Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatalf("PutSource: %v", err)
	}
	return s
}

// Every read path must project the description. A query site that forgets the
// new column would otherwise silently serve an empty description.
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

	t.Run("ListResourceRevisions", func(t *testing.T) {
		got, err := s.ListResourceRevisions(ctx, rev.Coordinate)
		if err != nil {
			t.Fatal(err)
		}
		assertSingleDescription(t, got)
	})

	t.Run("ListResourceRevisionsByArchiveDigest", func(t *testing.T) {
		got, err := s.ListResourceRevisionsByArchiveDigest(ctx, "mp", rev.ArchiveDigest)
		if err != nil {
			t.Fatal(err)
		}
		assertSingleDescription(t, got)
	})

	t.Run("ListResourceRevisionsByContentDigest", func(t *testing.T) {
		got, err := s.ListResourceRevisionsByContentDigest(ctx, "mp", rev.ContentDigest)
		if err != nil {
			t.Fatal(err)
		}
		assertSingleDescription(t, got)
	})

	t.Run("SearchResourceRevisions", func(t *testing.T) {
		got, err := s.SearchResourceRevisions(ctx, store.ResourceSearchFilter{Marketplace: "mp", Query: "review"})
		if err != nil {
			t.Fatal(err)
		}
		assertSingleDescription(t, got)
	})
}

func assertDescription(t *testing.T, got string) {
	t.Helper()
	if got != testDescription {
		t.Fatalf("description = %q, want %q", got, testDescription)
	}
}

func assertSingleDescription(t *testing.T, revs []store.ResourceRevision) {
	t.Helper()
	if len(revs) != 1 {
		t.Fatalf("got %d revisions, want 1", len(revs))
	}
	assertDescription(t, revs[0].Description)
}

// The store is the last line of defense for an unbounded or hostile
// description, whatever the caller passes in.
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

// A deployed instance already holds rows written before the description column
// existed. Opening such a database must migrate it in place without losing
// data.
func TestMigrationAddsDescriptionToPreExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	// Build the schema as it existed before the description column, including
	// the migration bookkeeping rows that a deployed instance would carry.
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE marketplace_schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`,
		`INSERT INTO marketplace_schema_migrations (version, applied_at) VALUES (0, 1), (1, 1), (2, 1)`,
		`CREATE TABLE marketplaces (id TEXT PRIMARY KEY, name TEXT NOT NULL, canonical_url TEXT NOT NULL, instance_metadata TEXT NOT NULL DEFAULT '{}')`,
		`CREATE TABLE sources (
			id TEXT PRIMARY KEY, slug TEXT NOT NULL, aliases TEXT NOT NULL DEFAULT '[]',
			origins TEXT NOT NULL DEFAULT '[]', ref TEXT NOT NULL, roots TEXT NOT NULL DEFAULT '[]',
			acquisition TEXT NOT NULL, publisher TEXT NOT NULL DEFAULT '{}', license TEXT,
			credentials_ref TEXT, policy_overrides TEXT NOT NULL DEFAULT '{}',
			listed INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
			refresh_status TEXT NOT NULL DEFAULT 'unknown', refresh_error TEXT,
			refreshed_at INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE resource_revisions (
			marketplace_id TEXT NOT NULL, source_id TEXT NOT NULL, kind TEXT NOT NULL, name TEXT NOT NULL,
			content_digest TEXT NOT NULL, archive_digest TEXT, analyzer_version TEXT,
			findings TEXT NOT NULL DEFAULT '[]', license TEXT, provenance TEXT, revocation TEXT,
			created_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (marketplace_id, source_id, kind, name, content_digest))`,
		`CREATE TABLE source_occurrences (
			source_id TEXT NOT NULL, ref TEXT NOT NULL, revision_commit TEXT NOT NULL,
			manifest_digest TEXT NOT NULL, acquisition TEXT NOT NULL, mirrored_artifact_digest TEXT,
			occurred_at INTEGER NOT NULL, PRIMARY KEY (source_id, revision_commit))`,
		`CREATE TABLE collection_revisions (
			id TEXT PRIMARY KEY, source_id TEXT NOT NULL, manifest_digest TEXT NOT NULL,
			members TEXT NOT NULL DEFAULT '[]', created_at INTEGER NOT NULL)`,
		`CREATE TABLE revocations (
			kind TEXT NOT NULL, target_id TEXT NOT NULL, reason TEXT NOT NULL,
			applied_at INTEGER NOT NULL, PRIMARY KEY (kind, target_id))`,
		`CREATE TABLE jobs (
			id TEXT PRIMARY KEY, kind TEXT NOT NULL, payload BLOB NOT NULL, status TEXT NOT NULL,
			lease_until INTEGER, attempt INTEGER NOT NULL DEFAULT 0, max_attempts INTEGER NOT NULL DEFAULT 1,
			backoff INTEGER NOT NULL DEFAULT 0, idempotency_key TEXT, worker_id TEXT,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`INSERT INTO marketplaces (id, name, canonical_url) VALUES ('mp', 'Marketplace', 'https://mp.test')`,
		`INSERT INTO sources (id, slug, ref, acquisition, listed, created_at, updated_at)
			VALUES ('src', 'src', 'main', 'mirror', 1, 1, 1)`,
		`INSERT INTO resource_revisions
			(marketplace_id, source_id, kind, name, content_digest, archive_digest, license, created_at)
			VALUES ('mp', 'src', 'skill', 'legacy', 'sha256:` + strings.Repeat("c", 64) + `', 'sha256:` + strings.Repeat("d", 64) + `', 'Apache-2.0', 1)`,
	} {
		if _, err := legacy.ExecContext(ctx, statement); err != nil {
			_ = legacy.Close()
			t.Fatalf("seed legacy schema: %v\nstatement: %s", err, statement)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	// Opening through the migrator must upgrade the database in place.
	s := sqlite.New(path)
	if err := s.Open(ctx); err != nil {
		t.Fatalf("Open pre-existing database: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	coord := store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: "legacy"}
	got, err := s.GetLatestResourceRevision(ctx, coord)
	if err != nil {
		t.Fatalf("read migrated row: %v", err)
	}
	if got.Description != "" {
		t.Fatalf("migrated row description = %q, want empty", got.Description)
	}
	if got.License != "Apache-2.0" {
		t.Fatalf("migrated row lost data: license = %q, want Apache-2.0", got.License)
	}
	if got.ArchiveDigest != store.ArchiveDigest("sha256:"+strings.Repeat("d", 64)) {
		t.Fatalf("migrated row lost its archive digest: %q", got.ArchiveDigest)
	}

	// The upgraded database must accept and return descriptions.
	updated := got
	updated.Description = testDescription
	if err := s.PutResourceRevision(ctx, updated); err != nil {
		t.Fatalf("write description to migrated database: %v", err)
	}
	reread, err := s.GetLatestResourceRevision(ctx, coord)
	if err != nil {
		t.Fatal(err)
	}
	assertDescription(t, reread.Description)

	// Re-opening an already-migrated database must be a no-op, not an error.
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened := sqlite.New(path)
	if err := reopened.Open(ctx); err != nil {
		t.Fatalf("re-open migrated database: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	final, err := reopened.GetLatestResourceRevision(ctx, coord)
	if err != nil {
		t.Fatal(err)
	}
	assertDescription(t, final.Description)
}
