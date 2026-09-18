package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/s3blob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func testSchemaName(t *testing.T) string {
	s := strings.ToLower(t.Name())
	return regexp.MustCompile(`[^a-z0-9_]`).ReplaceAllString(s, "_")
}

func openStores(t *testing.T) (store.MetadataStore, store.BlobStore) {
	t.Helper()

	postgresDSN := os.Getenv("POSTGRES_DSN")
	s3Endpoint := os.Getenv("S3_TEST_ENDPOINT")
	s3Bucket := os.Getenv("S3_TEST_BUCKET")
	s3Region := os.Getenv("S3_TEST_REGION")

	if postgresDSN != "" && s3Endpoint != "" && s3Bucket != "" && s3Region != "" {
		schema := testSchemaName(t)
		meta := postgres.NewWithSchema(postgresDSN, schema)
		if err := meta.Open(context.Background()); err != nil {
			t.Fatalf("Open PostgreSQL metadata store: %v", err)
		}
		t.Cleanup(func() { _ = meta.Close() })
		t.Cleanup(func() {
			db, err := sql.Open("pgx", postgresDSN)
			if err != nil {
				return
			}
			defer func() { _ = db.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		})

		blobs, err := s3blob.New(context.Background(), s3Bucket, s3Region, s3blob.WithEndpoint(s3Endpoint))
		if err != nil {
			t.Fatalf("New S3 blob store: %v", err)
		}
		return meta, blobs
	}

	dir := t.TempDir()
	meta := sqlite.New(filepath.Join(dir, "marketplace.db"))
	if err := meta.Open(context.Background()); err != nil {
		t.Fatalf("Open metadata store: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	blobs := fsblob.New(filepath.Join(dir, "blobs"))
	return meta, blobs
}

func TestConformance(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openStores(t)

	mp := store.Marketplace{
		ID:           store.MarketplaceID("mp-conf"),
		Name:         "Conformance Marketplace",
		CanonicalURL: "https://marketplace.conf.test",
	}
	if err := meta.PutMarketplace(ctx, mp); err != nil {
		t.Fatalf("PutMarketplace: %v", err)
	}

	src := store.Registry{
		ID:          store.RegistryID("src-conf"),
		Slug:        "conformance-registry",
		Ref:         "main",
		Acquisition: store.AcquisitionMirror,
		Listed:      true,
	}
	if err := meta.PutRegistry(ctx, src); err != nil {
		t.Fatalf("PutRegistry: %v", err)
	}

	coord := store.ResourceCoordinate{
		Marketplace: mp.ID,
		Registry:    src.ID,
		Kind:        "skill",
		Name:        "member",
	}
	rev := store.ResourceRevision{
		Coordinate:    coord,
		ContentDigest: store.RevisionDigest("sha256:member-content"),
		ArchiveDigest: store.ArchiveDigest("sha256:member-archive"),
		License:       "MIT",
	}
	if err := meta.PutResourceRevision(ctx, rev); err != nil {
		t.Fatalf("PutResourceRevision: %v", err)
	}

	// The integrity check should fail before blobs are uploaded.
	err := meta.CheckIntegrity(ctx, blobs)
	if !errors.Is(err, store.ErrBlobMissing) {
		t.Fatalf("CheckIntegrity before blobs error = %v, want ErrBlobMissing", err)
	}

	if err := blobs.Put(ctx, rev.ArchiveDigest, []byte("member archive bytes")); err != nil {
		t.Fatalf("Put blob: %v", err)
	}

	if err := meta.CheckIntegrity(ctx, blobs); err != nil {
		t.Fatalf("CheckIntegrity after blob: %v", err)
	}

	occ := store.RegistryOccurrence{
		RegistryID:                     src.ID,
		Ref:                            "main",
		Commit:                         "deadbeef",
		RegistryRevisionManifestDigest: store.RevisionDigest("sha256:manifest"),
		Acquisition:                    store.AcquisitionMirror,
		MirroredArtifactDigest:         store.ArchiveDigest("sha256:occ-archive"),
		OccurredAt:                     time.Now().UTC(),
	}
	if err := meta.PutRegistryOccurrence(ctx, occ); err != nil {
		t.Fatalf("PutRegistryOccurrence: %v", err)
	}

	err = meta.CheckIntegrity(ctx, blobs)
	if !errors.Is(err, store.ErrBlobMissing) {
		t.Fatalf("CheckIntegrity before occurrence blob error = %v, want ErrBlobMissing", err)
	}

	if err := blobs.Put(ctx, store.ArchiveDigest(occ.RegistryRevisionManifestDigest), []byte("manifest bytes")); err != nil {
		t.Fatalf("Put manifest blob: %v", err)
	}

	err = meta.CheckIntegrity(ctx, blobs)
	if !errors.Is(err, store.ErrBlobMissing) {
		t.Fatalf("CheckIntegrity before occurrence blob error = %v, want ErrBlobMissing", err)
	}

	if err := blobs.Put(ctx, occ.MirroredArtifactDigest, []byte("occurrence archive bytes")); err != nil {
		t.Fatalf("Put occurrence blob: %v", err)
	}

	if err := meta.CheckIntegrity(ctx, blobs); err != nil {
		t.Fatalf("CheckIntegrity after all blobs: %v", err)
	}
}

func TestConformancePutRegistrySlugCollisionPreservesExistingRegistry(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	first := store.Registry{ID: "registry-first", Slug: "shared-slug", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}
	second := store.Registry{ID: "registry-second", Slug: "shared-slug", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}
	if err := meta.PutRegistry(ctx, first); err != nil {
		t.Fatalf("PutRegistry(first): %v", err)
	}
	if err := meta.PutRegistry(ctx, second); err == nil {
		t.Fatal("PutRegistry(second) unexpectedly succeeded despite slug collision")
	}
	got, err := meta.GetRegistry(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetRegistry(first) after collision: %v", err)
	}
	if got.ID != first.ID || got.Slug != first.Slug {
		t.Fatalf("existing registry after slug collision = %#v, want %#v", got, first)
	}
	if _, err := meta.GetRegistry(ctx, second.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second registry lookup error = %v, want ErrNotFound", err)
	}
}

func TestConformanceLeaseJobByIDLeavesOlderJobPending(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	jobs := []store.Job{
		{ID: "older", Kind: "index-registry", Payload: []byte(`{}`), MaxAttempts: 1, CreatedAt: time.Now().Add(-time.Hour)},
		{ID: "target", Kind: "index-registry", Payload: []byte(`{}`), MaxAttempts: 1},
	}
	for _, item := range jobs {
		if err := meta.EnqueueJob(ctx, item); err != nil {
			t.Fatalf("EnqueueJob: %v", err)
		}
	}
	leased, err := meta.LeaseJobByID(ctx, jobs[1].ID, "worker", time.Minute)
	if err != nil {
		t.Fatalf("LeaseJobByID: %v", err)
	}
	if leased.ID != jobs[1].ID {
		t.Fatalf("leased job = %q, want %q", leased.ID, jobs[1].ID)
	}
	older, err := meta.GetJob(ctx, jobs[0].ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if older.Status != store.JobStatusPending {
		t.Fatalf("older job status = %q, want %q", older.Status, store.JobStatusPending)
	}
}

func TestConformanceConcurrentDuplicateIdempotencyKeysReturnConflict(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	const attempts = 8
	start := make(chan struct{})
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			<-start
			errs <- meta.EnqueueJob(ctx, store.Job{ID: store.JobID(fmt.Sprintf("raced-%d", i)), Kind: "refresh", IdempotencyKey: "same-refresh", Payload: []byte("payload")})
		}(i)
	}
	close(start)
	var successes int
	for i := 0; i < attempts; i++ {
		err := <-errs
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("raced duplicate enqueue error = %v, want ErrConflict", err)
		}
	}
	if successes != 1 {
		t.Fatalf("raced duplicate enqueue successes = %d, want 1", successes)
	}
}

func TestConformanceJobLeaseExpiry(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)

	job := store.Job{
		ID:          store.JobID("job-conf"),
		Kind:        "index-registry",
		Payload:     []byte(`{"registry":"src-conf"}`),
		MaxAttempts: 3,
	}
	if err := meta.EnqueueJob(ctx, job); err != nil {
		t.Fatalf("EnqueueJob: %v", err)
	}

	leased, err := meta.LeaseJob(ctx, "index-registry", "worker-a", 2*time.Millisecond)
	if err != nil {
		t.Fatalf("LeaseJob: %v", err)
	}
	if leased.Attempt != 1 {
		t.Fatalf("first attempt = %d, want 1", leased.Attempt)
	}

	time.Sleep(5 * time.Millisecond)

	recovered, err := meta.LeaseJob(ctx, "index-registry", "worker-b", time.Minute)
	if err != nil {
		t.Fatalf("LeaseJob recovery: %v", err)
	}
	if recovered.ID != job.ID {
		t.Fatalf("recovered job id %q, want %q", recovered.ID, job.ID)
	}
	if recovered.WorkerID != "worker-b" {
		t.Fatalf("recovered worker %q, want worker-b", recovered.WorkerID)
	}
	if recovered.Attempt != 2 {
		t.Fatalf("recovered attempt %d, want 2", recovered.Attempt)
	}
}

func TestConformanceCatalogProjectionPersistsProvenanceAndPages(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)
	const (
		marketplace = store.MarketplaceID("projection-mp")
		registry    = store.RegistryID("projection-reg")
		commit      = "projection-commit"
	)
	catalogVersion := func() (store.CatalogVersion, error) {
		page, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: marketplace, Limit: 1})
		return page.CatalogVersion, err
	}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: marketplace, Name: "Projection", CanonicalURL: "https://marketplace.test"}); err != nil {
		t.Fatal(err)
	}
	beforeRollback, err := catalogVersion()
	if err != nil {
		t.Fatal(err)
	}
	err = meta.WithTx(ctx, func(txCtx context.Context, tx store.MetadataStore) error {
		if err := tx.PutRegistry(txCtx, store.Registry{ID: "rolled-back", Slug: "rolled-back", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	if err == nil {
		t.Fatal("transaction unexpectedly succeeded")
	}
	afterRollback, err := catalogVersion()
	if err != nil {
		t.Fatal(err)
	}
	if afterRollback != beforeRollback {
		t.Fatalf("rolled-back publication changed catalog version from %s to %s", beforeRollback, afterRollback)
	}
	if err := meta.PutRegistry(ctx, store.Registry{ID: registry, Slug: "projection", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}); err != nil {
		t.Fatal(err)
	}
	manifest := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	legacyOccurrence := store.RegistryOccurrence{RegistryID: registry, Ref: "main", Commit: commit, RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror}
	if err := meta.PutRegistryOccurrence(ctx, legacyOccurrence); err != nil {
		t.Fatal(err)
	}
	enrichedOccurrence := legacyOccurrence
	enrichedOccurrence.SourceOrigin = "https://example.test/agents/resources.git?token=must-not-persist&ref=main"
	if err := meta.PutRegistryOccurrence(ctx, enrichedOccurrence); err != nil {
		t.Fatalf("enrich registry occurrence: %v", err)
	}
	gotOccurrence, err := meta.GetRegistryOccurrence(ctx, registry, commit)
	if err != nil {
		t.Fatal(err)
	}
	if gotOccurrence.SourceOrigin != "https://example.test/agents/resources.git?ref=main" {
		t.Fatalf("source origin = %q, want sanitized public URL", gotOccurrence.SourceOrigin)
	}
	beforeOccurrenceRetry, err := catalogVersion()
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.PutRegistryOccurrence(ctx, enrichedOccurrence); err != nil {
		t.Fatalf("registry occurrence retry: %v", err)
	}
	afterOccurrenceRetry, err := catalogVersion()
	if err != nil {
		t.Fatal(err)
	}
	if afterOccurrenceRetry != beforeOccurrenceRetry {
		t.Fatalf("idempotent registry occurrence changed catalog version from %s to %s", beforeOccurrenceRetry, afterOccurrenceRetry)
	}
	conflictingOccurrence := enrichedOccurrence
	conflictingOccurrence.SourceOrigin = "https://other.test/agents/resources.git"
	if err := meta.PutRegistryOccurrence(ctx, conflictingOccurrence); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("conflicting source origin error = %v, want ErrConflict", err)
	}

	putCurrent := func(kind, name, digestChar, sourcePath string) store.ResourceRevision {
		t.Helper()
		rev := store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: marketplace, Registry: registry, Kind: kind, Name: name}, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat(digestChar, 64)), Description: name}
		if err := meta.PutResourceRevision(ctx, rev); err != nil {
			t.Fatal(err)
		}
		if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, Commit: commit, ManifestDigest: manifest, SourcePath: sourcePath}); err != nil {
			t.Fatal(err)
		}
		assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
			Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest, AnalyzerVersion: "projection/v1",
			PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("b", 64)), Decision: store.AssessmentDecisionAllow,
			InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("c", 64)), AssessedAt: time.Unix(1, 0).UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := meta.PutResourceAssessment(ctx, assessment); err != nil {
			t.Fatal(err)
		}
		if err := meta.SetCurrentResourceAssessment(ctx, assessment); err != nil {
			t.Fatal(err)
		}
		return rev
	}
	member := putCurrent("skill", "member", "d", "")
	if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: member.Coordinate, ContentDigest: member.ContentDigest, Commit: commit, ManifestDigest: manifest, SourcePath: "skills/member/SKILL.md"}); err != nil {
		t.Fatalf("enrich resource occurrence: %v", err)
	}
	if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: member.Coordinate, ContentDigest: member.ContentDigest, Commit: commit, ManifestDigest: manifest, SourcePath: "other/member/SKILL.md"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("conflicting source path error = %v, want ErrConflict", err)
	}
	if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: member.Coordinate, ContentDigest: member.ContentDigest, Commit: commit, ManifestDigest: manifest, SourcePath: "../outside/SKILL.md"}); err == nil {
		t.Fatal("traversal source path unexpectedly accepted")
	}
	review := putCurrent("skill", "review", "e", "skills/review/SKILL.md")
	beforeRetry, err := catalogVersion()
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceOccurrence(ctx, store.ResourceOccurrence{Coordinate: review.Coordinate, ContentDigest: review.ContentDigest, Commit: commit, ManifestDigest: manifest, SourcePath: "skills/review/SKILL.md"}); err != nil {
		t.Fatalf("provenance retry: %v", err)
	}
	afterRetry, err := catalogVersion()
	if err != nil {
		t.Fatal(err)
	}
	if afterRetry != beforeRetry {
		t.Fatalf("idempotent resource occurrence changed catalog version from %s to %s", beforeRetry, afterRetry)
	}
	collectionDigest := store.RevisionDigest("sha256:" + strings.Repeat("f", 64))
	collection := putCurrent("collection", "bundle", "f", "collections/bundle/COLLECTION.yaml")
	if collection.ContentDigest != collectionDigest {
		t.Fatal("collection fixture digest changed unexpectedly")
	}
	memberCollection := putCurrent("collection", "member-collection", "a", "collections/member-collection/COLLECTION.yaml")
	missingMember := store.ResourceCoordinate{Marketplace: marketplace, Registry: registry, Kind: "collection", Name: "missing-member"}
	missingInclude := store.ResourceCoordinate{Marketplace: marketplace, Registry: registry, Kind: "collection", Name: "missing-include"}
	if err := meta.PutCollectionRevision(ctx, store.CollectionRevision{
		ID: string(collectionDigest), RegistryID: registry, ManifestDigest: collectionDigest,
		Members:  []store.ResourceCoordinate{member.Coordinate, memberCollection.Coordinate, missingMember},
		Includes: []store.ResourceCoordinate{collection.Coordinate, missingInclude},
	}); err != nil {
		t.Fatal(err)
	}

	first, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: marketplace, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Resources) != 1 || first.Next == nil {
		t.Fatalf("first page = %+v, want one resource and a cursor", first)
	}
	second, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: marketplace, After: first.Next, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if second.CatalogVersion != first.CatalogVersion || len(second.Resources) != 3 || second.Next != nil {
		t.Fatalf("second page = %+v, want three final resources at version %s", second, first.CatalogVersion)
	}
	all := append(first.Resources, second.Resources...)
	if all[0].Coordinate.Name != "bundle" || all[1].Coordinate.Name != "member-collection" || all[2].Coordinate.Name != "member" || all[3].Coordinate.Name != "review" {
		t.Fatalf("coordinate order = %#v, want bundle, member-collection, member, review", []string{all[0].Coordinate.Name, all[1].Coordinate.Name, all[2].Coordinate.Name, all[3].Coordinate.Name})
	}
	if all[0].Collection == nil ||
		len(all[0].Collection.Resources) != 2 ||
		all[0].Collection.Resources[0] != member.Coordinate ||
		all[0].Collection.Resources[1] != memberCollection.Coordinate ||
		len(all[0].Collection.Includes) != 1 ||
		all[0].Collection.Includes[0] != collection.Coordinate ||
		len(all[0].Collection.UnresolvedReferences) != 2 ||
		all[0].Collection.UnresolvedReferences[0] != (store.CatalogUnresolvedReference{ReferenceType: "resource", Coordinate: missingMember, Reason: "not_found"}) ||
		all[0].Collection.UnresolvedReferences[1] != (store.CatalogUnresolvedReference{ReferenceType: "include", Coordinate: missingInclude, Reason: "not_found"}) {
		t.Fatalf("direct collection relations = %+v, want distinct direct members, includes, and typed unresolved references", all[0].Collection)
	}
	if all[3].Source == nil || all[3].Source.RepositoryURL != gotOccurrence.SourceOrigin || all[3].Source.Path != "skills/review/SKILL.md" {
		t.Fatalf("resource provenance = %+v, want sanitized source", all[3])
	}
	blocked, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: member.Coordinate, ContentDigest: member.ContentDigest, AnalyzerVersion: "projection/v2",
		PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("1", 64)), Decision: store.AssessmentDecisionReject,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("2", 64)), AssessedAt: time.Unix(2, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceAssessment(ctx, blocked); err != nil {
		t.Fatal(err)
	}
	if err := meta.SetCurrentResourceAssessment(ctx, blocked); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutRevocation(ctx, store.Revocation{Kind: store.RevocationCoordinate, TargetID: review.Coordinate.String(), Reason: "operator test"}); err != nil {
		t.Fatal(err)
	}
	availability, err := meta.ReadCatalogProjectionPage(ctx, store.CatalogProjectionPageRequest{Marketplace: marketplace, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if availability.Resources[2].Availability != store.CatalogAvailabilityBlocked || availability.Resources[3].Availability != store.CatalogAvailabilityRevoked {
		t.Fatalf("availability = %q, %q; want blocked, revoked", availability.Resources[2].Availability, availability.Resources[3].Availability)
	}
	afterPublication, err := catalogVersion()
	if err != nil {
		t.Fatal(err)
	}
	if afterPublication <= beforeRollback {
		t.Fatalf("catalog version = %s, want a monotonic advance after visible publication", afterPublication)
	}
}
