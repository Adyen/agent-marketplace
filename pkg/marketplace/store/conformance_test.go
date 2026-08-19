package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func openStores(t *testing.T) (*sqlite.Store, *fsblob.Store) {
	t.Helper()
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

	src := store.Source{
		ID:          store.SourceID("src-conf"),
		Slug:        "conformance-source",
		Ref:         "main",
		Acquisition: store.AcquisitionMirror,
		Listed:      true,
	}
	if err := meta.PutSource(ctx, src); err != nil {
		t.Fatalf("PutSource: %v", err)
	}

	coord := store.ResourceCoordinate{
		Marketplace: mp.ID,
		Source:      src.ID,
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

	// Readiness should fail before blobs are uploaded.
	err := meta.ReadyCheck(ctx, blobs)
	if !errors.Is(err, store.ErrBlobMissing) {
		t.Fatalf("ReadyCheck before blobs error = %v, want ErrBlobMissing", err)
	}

	if err := blobs.Put(ctx, rev.ArchiveDigest, []byte("member archive bytes")); err != nil {
		t.Fatalf("Put blob: %v", err)
	}

	if err := meta.ReadyCheck(ctx, blobs); err != nil {
		t.Fatalf("ReadyCheck after blob: %v", err)
	}

	occ := store.SourceOccurrence{
		SourceID:                     src.ID,
		Ref:                          "main",
		Commit:                       "deadbeef",
		SourceRevisionManifestDigest: store.RevisionDigest("sha256:manifest"),
		Acquisition:                  store.AcquisitionMirror,
		MirroredArtifactDigest:       store.ArchiveDigest("sha256:occ-archive"),
		OccurredAt:                   time.Now().UTC(),
	}
	if err := meta.PutSourceOccurrence(ctx, occ); err != nil {
		t.Fatalf("PutSourceOccurrence: %v", err)
	}

	err = meta.ReadyCheck(ctx, blobs)
	if !errors.Is(err, store.ErrBlobMissing) {
		t.Fatalf("ReadyCheck before occurrence blob error = %v, want ErrBlobMissing", err)
	}

	if err := blobs.Put(ctx, store.ArchiveDigest(occ.SourceRevisionManifestDigest), []byte("manifest bytes")); err != nil {
		t.Fatalf("Put manifest blob: %v", err)
	}

	err = meta.ReadyCheck(ctx, blobs)
	if !errors.Is(err, store.ErrBlobMissing) {
		t.Fatalf("ReadyCheck before occurrence blob error = %v, want ErrBlobMissing", err)
	}

	if err := blobs.Put(ctx, occ.MirroredArtifactDigest, []byte("occurrence archive bytes")); err != nil {
		t.Fatalf("Put occurrence blob: %v", err)
	}

	if err := meta.ReadyCheck(ctx, blobs); err != nil {
		t.Fatalf("ReadyCheck after all blobs: %v", err)
	}
}

func TestConformanceJobLeaseExpiry(t *testing.T) {
	ctx := context.Background()
	meta, _ := openStores(t)

	job := store.Job{
		ID:          store.JobID("job-conf"),
		Kind:        "index-source",
		Payload:     []byte(`{"source":"src-conf"}`),
		MaxAttempts: 3,
	}
	if err := meta.EnqueueJob(ctx, job); err != nil {
		t.Fatalf("EnqueueJob: %v", err)
	}

	leased, err := meta.LeaseJob(ctx, "index-source", "worker-a", 2*time.Millisecond)
	if err != nil {
		t.Fatalf("LeaseJob: %v", err)
	}
	if leased.Attempt != 1 {
		t.Fatalf("first attempt = %d, want 1", leased.Attempt)
	}

	time.Sleep(5 * time.Millisecond)

	recovered, err := meta.LeaseJob(ctx, "index-source", "worker-b", time.Minute)
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
