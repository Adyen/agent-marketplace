package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestOpenErrorDoesNotLeakURLCredentials(t *testing.T) {
	const secret = "postgres://marketplace:startup-secret@%zz"
	s := postgres.New(secret)
	err := s.Open(context.Background())
	if err == nil {
		t.Fatal("Open unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), "startup-secret") {
		t.Fatalf("Open error leaked DSN credential: %v", err)
	}
}

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set; skipping PostgreSQL tests")
	}
	return dsn
}

func testSchema(t *testing.T) string {
	s := strings.ToLower(t.Name())
	return regexp.MustCompile(`[^a-z0-9_]`).ReplaceAllString(s, "_")
}

func dropSchema(t *testing.T, dsn, schema string) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("dropSchema open: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	if err != nil {
		t.Fatalf("dropSchema: %v", err)
	}
}

func newTestStore(t *testing.T) (*postgres.Store, string) {
	t.Helper()
	dsn := testDSN(t)
	schema := testSchema(t)
	s := postgres.NewWithSchema(dsn, schema)
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { dropSchema(t, dsn, schema) })
	t.Cleanup(func() { _ = s.Close() })
	return s, schema
}

func TestMigrationsAreRerunnable(t *testing.T) {
	dsn := testDSN(t)
	schema := testSchema(t)
	for i := 0; i < 2; i++ {
		s := postgres.NewWithSchema(dsn, schema)
		if err := s.Open(context.Background()); err != nil {
			t.Fatalf("Open %d: %v", i, err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close %d: %v", i, err)
		}
	}
	dropSchema(t, dsn, schema)
}

func TestCollectionIncludesMigrationDefaultsExistingRows(t *testing.T) {
	ctx := context.Background()
	dsn := testDSN(t)
	schema := testSchema(t)
	quoted := pgx.Identifier{schema}.Sanitize()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	t.Cleanup(func() { dropSchema(t, dsn, schema) })
	migrated := postgres.NewWithSchema(dsn, schema)
	if err := migrated.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if err := migrated.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
SET search_path TO `+quoted+`;
ALTER TABLE collection_revisions DROP COLUMN includes;
DELETE FROM marketplace_metadata_v2_schema_migrations WHERE version = 2;
INSERT INTO collection_revisions (id, registry_id, manifest_digest, members, created_at)
VALUES ('legacy-collection', 'reg-1', 'sha256:legacy', '[]', 0);
`); err != nil {
		t.Fatal(err)
	}

	s := postgres.NewWithSchema(dsn, schema)
	if err := s.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	collection, err := s.GetCollectionRevision(ctx, "legacy-collection")
	if err != nil {
		t.Fatal(err)
	}
	if len(collection.Includes) != 0 {
		t.Fatalf("migrated legacy collection includes = %+v, want empty", collection.Includes)
	}
	includes := []store.ResourceCoordinate{{Marketplace: "mp", Registry: "reg-1", Kind: "collection", Name: "base"}}
	if err := s.PutCollectionRevision(ctx, store.CollectionRevision{
		ID: "legacy-collection", RegistryID: "reg-1", ManifestDigest: "sha256:legacy", Members: []store.ResourceCoordinate{}, Includes: includes,
	}); err != nil {
		t.Fatalf("enrich migrated collection includes: %v", err)
	}
	collection, err = s.GetCollectionRevision(ctx, "legacy-collection")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(collection.Includes, includes) {
		t.Fatalf("enriched legacy collection includes = %+v, want %+v", collection.Includes, includes)
	}
}

func TestMigrationsSerializeConcurrentStartup(t *testing.T) {
	dsn := testDSN(t)
	schema := testSchema(t)
	const count = 4
	stores := make([]*postgres.Store, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := range stores {
		stores[i] = postgres.NewWithSchema(dsn, schema)
		wg.Add(1)
		go func(s *postgres.Store) {
			defer wg.Done()
			errs <- s.Open(context.Background())
		}(stores[i])
	}
	wg.Wait()
	for i := 0; i < count; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Open %d: %v", i, err)
		}
	}
	for _, s := range stores {
		_ = s.Close()
	}
	dropSchema(t, dsn, schema)
}

func TestMarketplaceCRUD(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	m := store.Marketplace{
		ID:           store.MarketplaceID("mp-test"),
		Name:         "Test Marketplace",
		CanonicalURL: "https://marketplace.example",
		InstanceMetadata: map[string]any{
			"region": "eu-west-1",
		},
	}
	if err := s.PutMarketplace(ctx, m); err != nil {
		t.Fatalf("PutMarketplace: %v", err)
	}

	got, err := s.GetMarketplace(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetMarketplace: %v", err)
	}
	if got.Name != m.Name || got.CanonicalURL != m.CanonicalURL {
		t.Fatalf("GetMarketplace returned %+v, want %+v", got, m)
	}
	if got.InstanceMetadata["region"] != "eu-west-1" {
		t.Fatalf("instance metadata lost: %+v", got.InstanceMetadata)
	}
}

func TestRegistryCRUD(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	reg := store.Registry{
		ID:      store.RegistryID("reg-1"),
		Slug:    "example-resources",
		Aliases: []string{"legacy-resources"},
		Origins: []string{"https://github.com/example/resources.git"},
		Ref:     "main",
		Roots: []store.RegistryRoot{
			{Path: ".agents", SourceHarness: "factory"},
		},
		Acquisition: store.AcquisitionMirror,
		Publisher:   store.Publisher{Name: "Example"},
		License:     "MIT",
		Listed:      true,
	}
	if err := s.PutRegistry(ctx, reg); err != nil {
		t.Fatalf("PutRegistry: %v", err)
	}

	got, err := s.GetRegistry(ctx, reg.ID)
	if err != nil {
		t.Fatalf("GetRegistry: %v", err)
	}
	if got.Slug != reg.Slug || got.Ref != reg.Ref {
		t.Fatalf("GetRegistry returned %+v, want %+v", got, reg)
	}

	bySlug, err := s.GetRegistryBySlug(ctx, "example-resources")
	if err != nil {
		t.Fatalf("GetRegistryBySlug current slug: %v", err)
	}
	if bySlug.ID != reg.ID {
		t.Fatalf("GetRegistryBySlug returned %q, want %q", bySlug.ID, reg.ID)
	}

	byAlias, err := s.GetRegistryBySlug(ctx, "legacy-resources")
	if err != nil {
		t.Fatalf("GetRegistryBySlug alias: %v", err)
	}
	if byAlias.ID != reg.ID {
		t.Fatalf("GetRegistryBySlug by alias returned %q, want %q", byAlias.ID, reg.ID)
	}

	reg.Listed = false
	if err := s.PutRegistry(ctx, reg); err != nil {
		t.Fatalf("PutRegistry unlisted: %v", err)
	}
	listed, err := s.ListRegistries(ctx)
	if err != nil {
		t.Fatalf("ListRegistries: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("ListRegistries returned %d registries, want 0", len(listed))
	}
}

func TestRegistryWithoutAliases(t *testing.T) {
	ctx := context.Background()
	s, schema := newTestStore(t)
	reg := store.Registry{
		ID:          "reg-no-aliases",
		Slug:        "no-aliases",
		Ref:         "main",
		Acquisition: store.AcquisitionMirror,
		Publisher:   store.Publisher{Name: "Example"},
		Listed:      true,
	}
	if err := s.PutRegistry(ctx, reg); err != nil {
		t.Fatalf("PutRegistry: %v", err)
	}

	got, err := s.GetRegistryBySlug(ctx, reg.Slug)
	if err != nil {
		t.Fatalf("GetRegistryBySlug current slug: %v", err)
	}
	if got.ID != reg.ID {
		t.Fatalf("GetRegistryBySlug returned %q, want %q", got.ID, reg.ID)
	}

	db, err := sql.Open("pgx", testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	quoted := pgx.Identifier{schema}.Sanitize()
	var aliases string
	if err := db.QueryRowContext(ctx, "SELECT aliases FROM "+quoted+".registries WHERE id=$1", reg.ID).Scan(&aliases); err != nil {
		t.Fatal(err)
	}
	if aliases != "[]" {
		t.Fatalf("stored aliases = %q, want []", aliases)
	}

	if _, err := db.ExecContext(ctx, "UPDATE "+quoted+".registries SET aliases='null' WHERE id=$1", reg.ID); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetRegistryBySlug(ctx, reg.Slug)
	if err != nil {
		t.Fatalf("GetRegistryBySlug legacy scalar current slug: %v", err)
	}
	if got.ID != reg.ID {
		t.Fatalf("GetRegistryBySlug legacy scalar returned %q, want %q", got.ID, reg.ID)
	}
	if _, err := s.GetRegistryBySlug(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetRegistryBySlug missing with legacy scalar = %v, want ErrNotFound", err)
	}
}

func TestResourceRevisionWithTwoAssessmentsAndCurrentPointer(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	coord := store.ResourceCoordinate{Marketplace: "mp-test", Registry: "reg-1", Kind: "skill", Name: "grill-me"}
	rev := store.ResourceRevision{
		Coordinate: coord, ContentDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		ArchiveDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222", License: "MIT",
	}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("exact idempotent PutResourceRevision: %v", err)
	}
	conflict := rev
	conflict.License = "Apache-2.0"
	if err := s.PutResourceRevision(ctx, conflict); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("changed immutable revision error = %v, want ErrConflict", err)
	}
	manifest := store.RevisionDigest("sha256:3333333333333333333333333333333333333333333333333333333333333333")
	if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "abc123", ManifestDigest: manifest,
	}); err != nil {
		t.Fatal(err)
	}
	assessment := func(version, policy string, decision store.AssessmentDecision) store.ResourceAssessment {
		t.Helper()
		value, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
			Coordinate: coord, ContentDigest: rev.ContentDigest, AnalyzerVersion: version,
			PolicyDigest: store.PolicyDigest(policy), Decision: decision,
			Findings:                 []store.Finding{{Code: version, Severity: "warning", Policy: "flag", Message: "finding"}},
			InspectionManifestDigest: "sha256:6666666666666666666666666666666666666666666666666666666666666666",
			AssessedAt:               time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	first := assessment("analyzer/v1", "sha256:4444444444444444444444444444444444444444444444444444444444444444", store.AssessmentDecisionWarn)
	second := assessment("analyzer/v2", "sha256:5555555555555555555555555555555555555555555555555555555555555555", store.AssessmentDecisionAllow)
	for _, value := range []store.ResourceAssessment{first, second} {
		if err := s.PutResourceAssessment(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetCurrentResourceAssessment(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: coord.Registry, Ref: "main", Commit: "abc123", RegistryRevisionManifestDigest: manifest,
		Acquisition: store.AcquisitionMirror,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetLatestResourceRevision(ctx, coord)
	if err != nil {
		t.Fatal(err)
	}
	if got.Assessment.AssessmentDigest != first.AssessmentDigest {
		t.Fatalf("current assessment = %q, want first %q", got.Assessment.AssessmentDigest, first.AssessmentDigest)
	}
	if err := s.SetCurrentResourceAssessment(ctx, second); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetResourceRevision(ctx, coord, rev.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Assessment.AssessmentDigest != second.AssessmentDigest || got.Occurrence.Commit != "abc123" {
		t.Fatalf("projection = %+v, want second assessment and current occurrence", got)
	}
	assessments, err := s.ListResourceAssessments(ctx, coord, rev.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if len(assessments) != 2 {
		t.Fatalf("ListResourceAssessments returned %d, want 2", len(assessments))
	}
}

func TestRegistryOccurrenceCRUD(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	occ := store.RegistryOccurrence{
		RegistryID:                     store.RegistryID("reg-1"),
		Ref:                            "main",
		Roots:                          []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}},
		Commit:                         "abc123",
		RegistryRevisionManifestDigest: store.RevisionDigest("sha256:3333333333333333333333333333333333333333333333333333333333333333"),
		Acquisition:                    store.AcquisitionMirror,
		MirroredArtifactDigest:         store.ArchiveDigest("sha256:4444444444444444444444444444444444444444444444444444444444444444"),
		OccurredAt:                     time.Now().UTC().Add(-time.Hour),
	}
	if err := s.PutRegistryOccurrence(ctx, occ); err != nil {
		t.Fatalf("PutRegistryOccurrence: %v", err)
	}

	got, err := s.GetRegistryOccurrence(ctx, occ.RegistryID, occ.Commit)
	if err != nil {
		t.Fatalf("GetRegistryOccurrence: %v", err)
	}
	if got.Commit != occ.Commit || got.MirroredArtifactDigest != occ.MirroredArtifactDigest || len(got.Roots) != 1 || got.Roots[0] != occ.Roots[0] {
		t.Fatalf("GetRegistryOccurrence returned %+v, want %+v", got, occ)
	}

	latest, err := s.LatestRegistryOccurrence(ctx, occ.RegistryID)
	if err != nil {
		t.Fatalf("LatestRegistryOccurrence: %v", err)
	}
	if latest.Commit != occ.Commit {
		t.Fatalf("LatestRegistryOccurrence returned %q, want %q", latest.Commit, occ.Commit)
	}
}

func TestCollectionAndRevocationCRUD(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	cr := store.CollectionRevision{
		ID:             "coll-1",
		RegistryID:     store.RegistryID("reg-1"),
		ManifestDigest: store.RevisionDigest("sha256:5555555555555555555555555555555555555555555555555555555555555555"),
		Members: []store.ResourceCoordinate{
			{Marketplace: store.MarketplaceID("mp"), Registry: store.RegistryID("reg-1"), Kind: "skill", Name: "member"},
		},
		Includes: []store.ResourceCoordinate{
			{Marketplace: store.MarketplaceID("mp"), Registry: store.RegistryID("reg-1"), Kind: "collection", Name: "included"},
		},
	}
	if err := s.PutCollectionRevision(ctx, cr); err != nil {
		t.Fatalf("PutCollectionRevision: %v", err)
	}
	got, err := s.GetCollectionRevision(ctx, cr.ID)
	if err != nil {
		t.Fatalf("GetCollectionRevision: %v", err)
	}
	if len(got.Members) != 1 || got.Members[0].Name != "member" {
		t.Fatalf("collection members lost: %+v", got.Members)
	}
	if len(got.Includes) != 1 || got.Includes[0].Name != "included" {
		t.Fatalf("collection includes lost: %+v", got.Includes)
	}
	changed := cr
	changed.Includes = nil
	if err := s.PutCollectionRevision(ctx, changed); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("conflicting collection includes write = %v, want ErrConflict", err)
	}

	all, err := s.ListCollectionRevisions(ctx, cr.RegistryID)
	if err != nil {
		t.Fatalf("ListCollectionRevisions: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ListCollectionRevisions returned %d, want 1", len(all))
	}
	if len(all[0].Includes) != 1 || all[0].Includes[0].Name != "included" {
		t.Fatalf("ListCollectionRevisions lost includes: %+v", all[0].Includes)
	}

	r := store.Revocation{
		Kind:      store.RevocationCoordinate,
		TargetID:  "mp/reg-1/skill/member",
		Reason:    "license violation",
		AppliedAt: time.Now().UTC(),
	}
	if err := s.PutRevocation(ctx, r); err != nil {
		t.Fatalf("PutRevocation: %v", err)
	}
	gotR, err := s.GetRevocation(ctx, r.Kind, r.TargetID)
	if err != nil {
		t.Fatalf("GetRevocation: %v", err)
	}
	if gotR.Reason != r.Reason {
		t.Fatalf("GetRevocation returned %+v, want %+v", gotR, r)
	}
	revocations, err := s.ListRevocations(ctx, store.RevocationCoordinate)
	if err != nil {
		t.Fatalf("ListRevocations: %v", err)
	}
	if len(revocations) != 1 {
		t.Fatalf("ListRevocations returned %d, want 1", len(revocations))
	}
}

func TestJobCRUD(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	job := store.Job{
		ID:          store.JobID("job-1"),
		Kind:        "index-registry",
		Payload:     []byte(`{"registry":"reg-1"}`),
		MaxAttempts: 3,
	}
	if err := s.EnqueueJob(ctx, job); err != nil {
		t.Fatalf("EnqueueJob: %v", err)
	}

	got, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != store.JobStatusPending {
		t.Fatalf("GetJob status = %q, want pending", got.Status)
	}

	leased, err := s.LeaseJob(ctx, "index-registry", "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("LeaseJob: %v", err)
	}
	if leased.Status != store.JobStatusLeased || leased.WorkerID != "worker-1" {
		t.Fatalf("LeaseJob returned %+v", leased)
	}
	if leased.Attempt != 1 {
		t.Fatalf("LeaseJob attempt = %d, want 1", leased.Attempt)
	}

	if err := s.ExtendLease(ctx, job.ID, "worker-1", time.Minute); err != nil {
		t.Fatalf("ExtendLease: %v", err)
	}

	if err := s.CompleteJob(ctx, job.ID, "worker-1"); err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}
	completed, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob after complete: %v", err)
	}
	if completed.Status != store.JobStatusCompleted {
		t.Fatalf("status = %q, want completed", completed.Status)
	}

	// Cannot lease a completed job.
	_, err = s.LeaseJob(ctx, "index-registry", "worker-2", time.Minute)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("LeaseJob completed error = %v, want ErrNotFound", err)
	}
}

func TestJobIdempotency(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	job := store.Job{
		ID:             store.JobID("job-1"),
		Kind:           "index-registry",
		Payload:        []byte("x"),
		IdempotencyKey: "registry-reg-1-main",
	}
	if err := s.EnqueueJob(ctx, job); err != nil {
		t.Fatalf("EnqueueJob first: %v", err)
	}

	err := s.EnqueueJob(ctx, job)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("EnqueueJob duplicate error = %v, want ErrConflict", err)
	}
}

func TestWithTxRollback(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)

	err := s.WithTx(ctx, func(txCtx context.Context, tx store.MetadataStore) error {
		if err := tx.PutMarketplace(txCtx, store.Marketplace{
			ID:           store.MarketplaceID("mp-tx"),
			Name:         "Should Rollback",
			CanonicalURL: "https://example.com",
		}); err != nil {
			return err
		}
		return errors.New("forced rollback")
	})
	if err == nil || err.Error() != "forced rollback" {
		t.Fatalf("WithTx error = %v", err)
	}

	_, err = s.GetMarketplace(ctx, store.MarketplaceID("mp-tx"))
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetMarketplace after rollback error = %v, want ErrNotFound", err)
	}
}
