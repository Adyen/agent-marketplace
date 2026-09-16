package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func newTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	s := sqlite.New(filepath.Join(t.TempDir(), "marketplace.db"))
	if err := s.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigrationsAreRerunnable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.db")
	for i := 0; i < 3; i++ {
		s := sqlite.New(path)
		if err := s.Open(context.Background()); err != nil {
			t.Fatalf("Open %d: %v", i, err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close %d: %v", i, err)
		}
	}
}

func TestMigrationsSerializeConcurrentStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.db")
	const count = 6
	stores := make([]*sqlite.Store, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := range stores {
		stores[i] = sqlite.New(path)
		wg.Add(1)
		go func(s *sqlite.Store) {
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
	// The stores remain usable after concurrent migration startup. Exercise a
	// transaction through one handle and read it through another handle before
	// reopening the database.
	ctx := context.Background()
	if err := stores[0].WithTx(ctx, func(txCtx context.Context, tx store.MetadataStore) error {
		return tx.PutMarketplace(txCtx, store.Marketplace{ID: "after-startup", Name: "Concurrent", CanonicalURL: "https://example.com"})
	}); err != nil {
		t.Fatalf("transaction after concurrent startup: %v", err)
	}
	if _, err := stores[1].GetMarketplace(ctx, "after-startup"); err != nil {
		t.Fatalf("CRUD after concurrent startup: %v", err)
	}
	for _, s := range stores {
		_ = s.Close()
	}

	reopened := sqlite.New(path)
	if err := reopened.Open(ctx); err != nil {
		t.Fatalf("reopen after concurrent startup: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if _, err := reopened.GetMarketplace(ctx, "after-startup"); err != nil {
		t.Fatalf("CRUD after reopen: %v", err)
	}
}

func TestInMemoryCRUDAndTransaction(t *testing.T) {
	ctx := context.Background()
	s := sqlite.New(":memory:")
	if err := s.Open(ctx); err != nil {
		t.Fatalf("Open in-memory: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.WithTx(ctx, func(txCtx context.Context, tx store.MetadataStore) error {
		return tx.PutMarketplace(txCtx, store.Marketplace{ID: "memory", Name: "In memory", CanonicalURL: "https://example.com"})
	}); err != nil {
		t.Fatalf("in-memory transaction: %v", err)
	}
	if _, err := s.GetMarketplace(ctx, "memory"); err != nil {
		t.Fatalf("in-memory CRUD after transaction: %v", err)
	}
}

func TestMarketplaceCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

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
	s := newTestStore(t)

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

func TestResourceRevisionCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	coord := store.ResourceCoordinate{
		Marketplace: store.MarketplaceID("mp-test"),
		Registry:    store.RegistryID("reg-1"),
		Kind:        "skill",
		Name:        "grill-me",
	}
	rev := store.ResourceRevision{
		Coordinate:    coord,
		ContentDigest: store.RevisionDigest("sha256:1111111111111111111111111111111111111111111111111111111111111111"),
		ArchiveDigest: store.ArchiveDigest("sha256:2222222222222222222222222222222222222222222222222222222222222222"),
		License:       "MIT",
	}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("PutResourceRevision: %v", err)
	}
	manifest := store.RevisionDigest("sha256:3333333333333333333333333333333333333333333333333333333333333333")
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: coord.Registry, Ref: "main", Commit: "abc123", RegistryRevisionManifestDigest: manifest,
		Acquisition: store.AcquisitionMirror,
	}); err != nil {
		t.Fatalf("PutRegistryOccurrence: %v", err)
	}
	if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "abc123", ManifestDigest: manifest,
	}); err != nil {
		t.Fatalf("PutResourceOccurrence: %v", err)
	}
	assessment := store.ResourceAssessment{
		Coordinate: coord, ContentDigest: rev.ContentDigest, AnalyzerVersion: "analyzer/v1",
		PolicyDigest: "sha256:4444444444444444444444444444444444444444444444444444444444444444",
		Decision:     store.AssessmentDecisionWarn,
		Findings: []store.Finding{
			{Code: "hidden-control", Severity: "warning", Policy: "flag", Message: "found control"},
		},
		InspectionManifestDigest: "sha256:5555555555555555555555555555555555555555555555555555555555555555",
		AssessedAt:               time.Now().UTC(),
	}
	if err := s.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatalf("PutResourceAssessment: %v", err)
	}
	if err := s.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatalf("SetCurrentResourceAssessment: %v", err)
	}

	got, err := s.GetResourceRevision(ctx, coord, rev.ContentDigest)
	if err != nil {
		t.Fatalf("GetResourceRevision: %v", err)
	}
	if got.ContentDigest != rev.ContentDigest || got.ArchiveDigest != rev.ArchiveDigest {
		t.Fatalf("GetResourceRevision returned %+v, want %+v", got, rev)
	}
	if len(got.Assessment.Findings) != 1 || got.Assessment.Findings[0].Code != "hidden-control" {
		t.Fatalf("findings lost: %+v", got.Assessment.Findings)
	}
	if got.Occurrence.Commit != "abc123" {
		t.Fatalf("occurrence commit = %q, want abc123", got.Occurrence.Commit)
	}

	all, err := s.ListResourceRevisions(ctx, coord)
	if err != nil {
		t.Fatalf("ListResourceRevisions: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ListResourceRevisions returned %d revisions, want 1", len(all))
	}
}

func TestSameRevisionSupportsTwoAssessmentsAndMutableCurrentPointer(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: "review"}
	rev := store.ResourceRevision{
		Coordinate: coord, ContentDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ArchiveDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		License:       "MIT", Description: "original",
	}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("exact retry: %v", err)
	}
	conflicting := rev
	conflicting.License = "Apache-2.0"
	if err := s.PutResourceRevision(ctx, conflicting); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("conflicting revision error = %v, want ErrConflict", err)
	}

	manifest := store.RevisionDigest("sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{
		RegistryID: coord.Registry, Ref: "main", Commit: "commit-1", RegistryRevisionManifestDigest: manifest,
		Acquisition: store.AcquisitionMirror, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{
		Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "commit-1", ManifestDigest: manifest,
	}); err != nil {
		t.Fatal(err)
	}

	first := store.ResourceAssessment{
		Coordinate: coord, ContentDigest: rev.ContentDigest, AnalyzerVersion: "analyzer/v1",
		PolicyDigest:             "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		AssessedAt:               time.Now().UTC().Add(-time.Minute),
	}
	second := first
	second.AnalyzerVersion = "analyzer/v2"
	second.Decision = store.AssessmentDecisionWarn
	second.AssessedAt = time.Now().UTC()
	for _, assessment := range []store.ResourceAssessment{first, second} {
		if err := s.PutResourceAssessment(ctx, assessment); err != nil {
			t.Fatalf("PutResourceAssessment: %v", err)
		}
	}
	firstFinal, err := store.FinalizeResourceAssessment(first)
	if err != nil {
		t.Fatal(err)
	}
	secondFinal, err := store.FinalizeResourceAssessment(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstFinal.AssessmentDigest == secondFinal.AssessmentDigest {
		t.Fatal("distinct assessments received the same digest")
	}
	byDigest, err := s.GetResourceAssessment(ctx, secondFinal.AssessmentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if byDigest.AnalyzerVersion != second.AnalyzerVersion {
		t.Fatalf("GetResourceAssessment analyzer = %q, want %q", byDigest.AnalyzerVersion, second.AnalyzerVersion)
	}
	if err := s.SetCurrentResourceAssessment(ctx, first); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetResourceRevision(ctx, coord, rev.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Assessment.AssessmentDigest != firstFinal.AssessmentDigest {
		t.Fatalf("current assessment = %q, want first %q", got.Assessment.AssessmentDigest, firstFinal.AssessmentDigest)
	}
	if err := s.SetCurrentResourceAssessment(ctx, second); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetResourceRevision(ctx, coord, rev.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if got.Assessment.AssessmentDigest != secondFinal.AssessmentDigest {
		t.Fatalf("current assessment = %q, want second %q", got.Assessment.AssessmentDigest, secondFinal.AssessmentDigest)
	}
	assessments, err := s.ListResourceAssessments(ctx, coord, rev.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if len(assessments) != 2 {
		t.Fatalf("assessment history length = %d, want 2", len(assessments))
	}
	if got.License != rev.License || got.Description != rev.Description {
		t.Fatalf("assessment pointer update changed intrinsic revision: %+v", got.ResourceRevision)
	}
}

func TestRegistryHeadRemovesResourcesFromDiscovery(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.PutRegistry(ctx, store.Registry{ID: "reg", Slug: "registry", Listed: true, Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatal(err)
	}
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: "removed"}
	rev := store.ResourceRevision{Coordinate: coord, ContentDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"}
	if err := s.PutResourceRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}
	assessment := store.ResourceAssessment{
		Coordinate: coord, ContentDigest: rev.ContentDigest, AnalyzerVersion: "v1",
		PolicyDigest:             "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: "sha256:3333333333333333333333333333333333333333333333333333333333333333",
		AssessedAt:               time.Now().UTC(),
	}
	if err := s.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrentResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	firstManifest := store.RevisionDigest("sha256:4444444444444444444444444444444444444444444444444444444444444444")
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: "reg", Ref: "main", Commit: "one", RegistryRevisionManifestDigest: firstManifest, Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "one", ManifestDigest: firstManifest}); err != nil {
		t.Fatal(err)
	}
	if results, err := s.SearchResourceRevisions(ctx, store.ResourceSearchFilter{Marketplace: "mp"}); err != nil || len(results) != 1 {
		t.Fatalf("search at first head = %d, %v; want one result", len(results), err)
	}
	secondManifest := store.RevisionDigest("sha256:5555555555555555555555555555555555555555555555555555555555555555")
	if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: "reg", Ref: "main", Commit: "two", RegistryRevisionManifestDigest: secondManifest, Acquisition: store.AcquisitionMirror}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRegistryHead(ctx, "reg", "two", "one"); err != nil {
		t.Fatal(err)
	}
	if results, err := s.SearchResourceRevisions(ctx, store.ResourceSearchFilter{Marketplace: "mp"}); err != nil || len(results) != 0 {
		t.Fatalf("search after removal = %d, %v; want no results", len(results), err)
	}
	if _, err := s.GetLatestResourceRevision(ctx, coord); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetLatestResourceRevision after removal error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetResourceRevision(ctx, coord, rev.ContentDigest); err != nil {
		t.Fatalf("addressed historical revision disappeared: %v", err)
	}
}

func TestRegistryHeadRejectsStalePublication(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	manifest := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	for _, commit := range []string{"one", "two", "stale"} {
		if err := s.PutRegistryOccurrence(ctx, store.RegistryOccurrence{RegistryID: "reg", Ref: "main", Commit: commit, RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetRegistryHead(ctx, "reg", "two", "one"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRegistryHead(ctx, "reg", "stale", "one"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale head update error = %v, want ErrConflict", err)
	}
	head, err := s.LatestRegistryOccurrence(ctx, "reg")
	if err != nil || head.Commit != "two" {
		t.Fatalf("head = %q, %v; want two", head.Commit, err)
	}
}

func TestReadyCheckIncludesAssessmentInspectionManifest(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: "ready"}
	contentDigest := store.RevisionDigest("sha256:7777777777777777777777777777777777777777777777777777777777777777")
	body := []byte("# ready\n")
	projection, err := inspection.Build(coordinate, string(contentDigest), body, nil, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	inspectionDigest := store.RevisionDigest(projection.Manifest.ManifestDigest)
	assessment := store.ResourceAssessment{
		Coordinate: coordinate, ContentDigest: contentDigest, AnalyzerVersion: "v1",
		PolicyDigest: "sha256:8888888888888888888888888888888888888888888888888888888888888888",
		Decision:     store.AssessmentDecisionAllow, InspectionManifestDigest: inspectionDigest,
		AssessedAt: time.Now().UTC(),
	}
	if err := s.PutResourceRevision(ctx, store.ResourceRevision{Coordinate: assessment.Coordinate, ContentDigest: assessment.ContentDigest}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutResourceAssessment(ctx, assessment); err != nil {
		t.Fatal(err)
	}
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	if err := s.ReadyCheck(ctx, blobs); !errors.Is(err, store.ErrBlobMissing) {
		t.Fatalf("ReadyCheck without inspection blob error = %v, want ErrBlobMissing", err)
	}
	if err := blobs.Put(ctx, store.ArchiveDigest(inspectionDigest), projection.Payload); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadyCheck(ctx, blobs); !errors.Is(err, store.ErrBlobMissing) {
		t.Fatalf("ReadyCheck without inspection body error = %v, want ErrBlobMissing", err)
	}
	if err := blobs.Put(ctx, store.ArchiveDigest(inspection.Digest(body)), body); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadyCheck(ctx, blobs); err != nil {
		t.Fatalf("ReadyCheck with inspection projection: %v", err)
	}
}

func TestRegistryOccurrenceCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

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

	// A refresh computes a new observation time, but the timestamp is assigned
	// only on first publication and must not make the immutable observation
	// conflict.
	retry := occ
	retry.OccurredAt = occ.OccurredAt.Add(time.Minute)
	if err := s.PutRegistryOccurrence(ctx, retry); err != nil {
		t.Fatalf("PutRegistryOccurrence refresh retry: %v", err)
	}
	got, err = s.GetRegistryOccurrence(ctx, occ.RegistryID, occ.Commit)
	if err != nil {
		t.Fatalf("GetRegistryOccurrence after refresh retry: %v", err)
	}
	if got.OccurredAt.UnixMicro() != occ.OccurredAt.UnixMicro() {
		t.Fatalf("refresh retry changed occurred_at to %v, want %v", got.OccurredAt, occ.OccurredAt)
	}

	// A conflicting observation for the same registry/commit must not overwrite it.
	changed := occ
	changed.Ref = "release"
	if err := s.PutRegistryOccurrence(ctx, changed); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("PutRegistryOccurrence conflicting error = %v, want ErrConflict", err)
	}
	got, err = s.GetRegistryOccurrence(ctx, occ.RegistryID, occ.Commit)
	if err != nil {
		t.Fatalf("GetRegistryOccurrence after conflict: %v", err)
	}
	if got.Ref != occ.Ref {
		t.Fatalf("conflicting write changed ref to %q, want %q", got.Ref, occ.Ref)
	}
}

func TestRegistryOccurrenceConcurrentDuplicates(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	base := store.RegistryOccurrence{
		RegistryID:                     store.RegistryID("reg-concurrent"),
		Ref:                            "main",
		Commit:                         "concurrent-commit",
		RegistryRevisionManifestDigest: store.RevisionDigest("sha256:6666666666666666666666666666666666666666666666666666666666666666"),
		Acquisition:                    store.AcquisitionMirror,
		MirroredArtifactDigest:         store.ArchiveDigest("sha256:7777777777777777777777777777777777777777777777777777777777777777"),
	}

	const publishers = 16
	errs := make(chan error, publishers)
	var wg sync.WaitGroup
	wg.Add(publishers)
	for i := 0; i < publishers; i++ {
		go func(i int) {
			defer wg.Done()
			occ := base
			occ.OccurredAt = time.Now().UTC().Add(time.Duration(i) * time.Second)
			errs <- s.PutRegistryOccurrence(ctx, occ)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent duplicate publication: %v", err)
		}
	}

	history, err := s.RegistryHistory(ctx, base.RegistryID)
	if err != nil {
		t.Fatalf("RegistryHistory: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("RegistryHistory returned %d occurrences, want 1", len(history))
	}
	if !history[0].Equal(base) {
		t.Fatalf("stored occurrence = %+v, want immutable fields %+v", history[0], base)
	}
	if history[0].OccurredAt.IsZero() {
		t.Fatal("stored occurrence has zero occurred_at")
	}
}

func TestRegistryHistoryEmptyIsNonNil(t *testing.T) {
	history, err := newTestStore(t).RegistryHistory(context.Background(), store.RegistryID("missing-registry"))
	if err != nil {
		t.Fatalf("RegistryHistory: %v", err)
	}
	if history == nil {
		t.Fatal("RegistryHistory returned nil, want empty non-nil slice")
	}
	if len(history) != 0 {
		t.Fatalf("RegistryHistory returned %d entries, want 0", len(history))
	}
}

func TestCollectionAndRevocationCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	cr := store.CollectionRevision{
		ID:             "coll-1",
		RegistryID:     store.RegistryID("reg-1"),
		ManifestDigest: store.RevisionDigest("sha256:5555555555555555555555555555555555555555555555555555555555555555"),
		Members: []store.ResourceCoordinate{
			{Marketplace: store.MarketplaceID("mp"), Registry: store.RegistryID("reg-1"), Kind: "skill", Name: "member"},
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
	if err := s.PutCollectionRevision(ctx, cr); err != nil {
		t.Fatalf("idempotent collection retry failed: %v", err)
	}
	changed := cr
	changed.ManifestDigest = store.RevisionDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := s.PutCollectionRevision(ctx, changed); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("conflicting collection write = %v, want ErrConflict", err)
	}
	got, err = s.GetCollectionRevision(ctx, cr.ID)
	if err != nil || got.ManifestDigest != cr.ManifestDigest {
		t.Fatalf("conflicting collection write changed stored metadata: %+v, %v", got, err)
	}

	all, err := s.ListCollectionRevisions(ctx, cr.RegistryID)
	if err != nil {
		t.Fatalf("ListCollectionRevisions: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ListCollectionRevisions returned %d, want 1", len(all))
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
	if err := s.DeleteRevocation(ctx, r.Kind, r.TargetID); err != nil {
		t.Fatalf("DeleteRevocation: %v", err)
	}
	if _, err := s.GetRevocation(ctx, r.Kind, r.TargetID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetRevocation after delete = %v, want ErrNotFound", err)
	}
}

func TestJobCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

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
	s := newTestStore(t)

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
	s := newTestStore(t)

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
