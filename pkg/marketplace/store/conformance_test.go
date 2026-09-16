package store_test

import (
	"context"
	"database/sql"
	"errors"
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
