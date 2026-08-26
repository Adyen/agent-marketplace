package postgres_test

import (
	"context"
	"database/sql"
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
	if err := s.PutSource(ctx, store.Source{
		ID: "src", Slug: "src", Ref: "main", Listed: true, Acquisition: store.AcquisitionMirror,
	}); err != nil {
		t.Fatalf("PutSource: %v", err)
	}

	rev := store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "skill", Name: name},
		ContentDigest: store.RevisionDigest(digestOf("a")),
		ArchiveDigest: store.ArchiveDigest(digestOf("b")),
		Description:   description,
		License:       "MIT",
	}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("PutResourceRevision: %v", err)
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

	for name, list := range map[string]func() ([]store.ResourceRevision, error){
		"ListResourceRevisions": func() ([]store.ResourceRevision, error) {
			return s.ListResourceRevisions(ctx, rev.Coordinate)
		},
		"ListResourceRevisionsByArchiveDigest": func() ([]store.ResourceRevision, error) {
			return s.ListResourceRevisionsByArchiveDigest(ctx, "mp", rev.ArchiveDigest)
		},
		"ListResourceRevisionsByContentDigest": func() ([]store.ResourceRevision, error) {
			return s.ListResourceRevisionsByContentDigest(ctx, "mp", rev.ContentDigest)
		},
		"SearchResourceRevisions": func() ([]store.ResourceRevision, error) {
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

// Re-publishing a coordinate must update the description through the upsert's
// DO UPDATE branch rather than silently keeping the old value.
func TestPutResourceRevisionUpdatesDescriptionOnConflict(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	rev := seedDescriptionFixture(t, s, "review", "original description")

	rev.Description = testDescription
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("re-put: %v", err)
	}

	got, err := s.GetResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != testDescription {
		t.Fatalf("description after upsert = %q, want %q", got.Description, testDescription)
	}
}

// A deployed PostgreSQL instance already holds rows written before the
// description column existed. Opening such a schema must migrate it in place
// without losing data.
func TestMigrationAddsDescriptionToPreExistingSchema(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	schema := testSchema(t)
	t.Cleanup(func() { dropSchema(t, dsn, schema) })

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	quoted := pgx.Identifier{schema}.Sanitize()
	for _, statement := range []string{
		"DROP SCHEMA IF EXISTS " + quoted + " CASCADE",
		"CREATE SCHEMA " + quoted,
		"SET search_path TO " + quoted,
		`CREATE TABLE marketplace_schema_migrations (version INTEGER PRIMARY KEY, applied_at BIGINT NOT NULL)`,
		`INSERT INTO marketplace_schema_migrations (version, applied_at) VALUES (0, 1), (1, 1), (2, 1)`,
		`CREATE TABLE marketplaces (id TEXT PRIMARY KEY, name TEXT NOT NULL, canonical_url TEXT NOT NULL,
			instance_metadata TEXT NOT NULL DEFAULT '{}')`,
		`CREATE TABLE sources (
			id TEXT PRIMARY KEY, slug TEXT NOT NULL, aliases TEXT NOT NULL DEFAULT '[]',
			origins TEXT NOT NULL DEFAULT '[]', ref TEXT NOT NULL, roots TEXT NOT NULL DEFAULT '[]',
			acquisition TEXT NOT NULL, publisher TEXT NOT NULL DEFAULT '{}', license TEXT,
			credentials_ref TEXT, policy_overrides TEXT NOT NULL DEFAULT '{}',
			listed BOOLEAN NOT NULL DEFAULT true, created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL,
			refresh_status TEXT NOT NULL DEFAULT 'unknown', refresh_error TEXT,
			refreshed_at BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE resource_revisions (
			marketplace_id TEXT NOT NULL, source_id TEXT NOT NULL, kind TEXT NOT NULL, name TEXT NOT NULL,
			content_digest TEXT NOT NULL, archive_digest TEXT, analyzer_version TEXT,
			findings TEXT NOT NULL DEFAULT '[]', license TEXT, provenance TEXT, revocation TEXT,
			created_at BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY (marketplace_id, source_id, kind, name, content_digest))`,
		`CREATE TABLE source_occurrences (
			source_id TEXT NOT NULL, ref TEXT NOT NULL, revision_commit TEXT NOT NULL,
			manifest_digest TEXT NOT NULL, acquisition TEXT NOT NULL, mirrored_artifact_digest TEXT,
			occurred_at BIGINT NOT NULL, PRIMARY KEY (source_id, revision_commit))`,
		`CREATE TABLE collection_revisions (
			id TEXT PRIMARY KEY, source_id TEXT NOT NULL, manifest_digest TEXT NOT NULL,
			members TEXT NOT NULL DEFAULT '[]', created_at BIGINT NOT NULL)`,
		`CREATE TABLE revocations (
			kind TEXT NOT NULL, target_id TEXT NOT NULL, reason TEXT NOT NULL,
			applied_at BIGINT NOT NULL, PRIMARY KEY (kind, target_id))`,
		`CREATE TABLE jobs (
			id TEXT PRIMARY KEY, kind TEXT NOT NULL, payload BYTEA NOT NULL, status TEXT NOT NULL,
			lease_until BIGINT, attempt INTEGER NOT NULL DEFAULT 0, max_attempts INTEGER NOT NULL DEFAULT 1,
			backoff BIGINT NOT NULL DEFAULT 0, idempotency_key TEXT, worker_id TEXT,
			created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL)`,
		`INSERT INTO marketplaces (id, name, canonical_url) VALUES ('mp', 'Marketplace', 'https://mp.test')`,
		`INSERT INTO sources (id, slug, ref, acquisition, listed, created_at, updated_at)
			VALUES ('src', 'src', 'main', 'mirror', true, 1, 1)`,
		`INSERT INTO resource_revisions
			(marketplace_id, source_id, kind, name, content_digest, archive_digest, license, created_at)
			VALUES ('mp', 'src', 'skill', 'legacy', '` + digestOf("c") + `', '` + digestOf("d") + `', 'Apache-2.0', 1)`,
	} {
		timeout, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err := db.ExecContext(timeout, statement)
		cancel()
		if err != nil {
			t.Fatalf("seed legacy schema: %v\nstatement: %s", err, statement)
		}
	}

	s := postgres.NewWithSchema(dsn, schema)
	if err := s.Open(ctx); err != nil {
		t.Fatalf("Open pre-existing schema: %v", err)
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
	if got.ArchiveDigest != store.ArchiveDigest(digestOf("d")) {
		t.Fatalf("migrated row lost its archive digest: %q", got.ArchiveDigest)
	}

	updated := got
	updated.Description = testDescription
	if err := s.PutResourceRevision(ctx, updated); err != nil {
		t.Fatalf("write description to migrated schema: %v", err)
	}
	reread, err := s.GetLatestResourceRevision(ctx, coord)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Description != testDescription {
		t.Fatalf("description = %q, want %q", reread.Description, testDescription)
	}
}
