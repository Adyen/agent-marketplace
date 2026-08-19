package store_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func sha256Digest(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

func openAtomicStores(t *testing.T) (*sqlite.Store, *fsblob.Store) {
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

func TestPublishSourceRevision(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openAtomicStores(t)

	mp := store.Marketplace{
		ID:           store.MarketplaceID("mp-atomic"),
		Name:         "Atomic Marketplace",
		CanonicalURL: "https://atomic.test",
	}
	if err := meta.PutMarketplace(ctx, mp); err != nil {
		t.Fatalf("PutMarketplace: %v", err)
	}

	src := store.Source{
		ID:          store.SourceID("src-atomic"),
		Slug:        "atomic-source",
		Ref:         "main",
		Acquisition: store.AcquisitionMirror,
		Listed:      true,
	}
	if err := meta.PutSource(ctx, src); err != nil {
		t.Fatalf("PutSource: %v", err)
	}

	manifest := []byte(`{"schema_version":"v1","generator":"test"}`)
	manifestDigest := store.RevisionDigest(sha256Digest(manifest))
	archive := []byte("archive bytes")
	archiveDigest := store.ArchiveDigest("sha256:archive123")

	coord := store.ResourceCoordinate{
		Marketplace: mp.ID,
		Source:      src.ID,
		Kind:        "skill",
		Name:        "member",
	}
	rev := store.ResourceRevision{
		Coordinate:    coord,
		ContentDigest: store.RevisionDigest("sha256:content123"),
		ArchiveDigest: archiveDigest,
	}

	occ := store.SourceOccurrence{
		SourceID:                     src.ID,
		Ref:                          "main",
		Commit:                       "abc123",
		SourceRevisionManifestDigest: manifestDigest,
		Acquisition:                  store.AcquisitionMirror,
		MirroredArtifactDigest:       archiveDigest,
		OccurredAt:                   time.Now().UTC(),
	}

	req := store.SourceRevisionPublication{
		Occurrence:      occ,
		Resources:       []store.ResourceRevision{rev},
		ManifestPayload: manifest,
		ExtraBlobs:      []store.BlobUpload{{Digest: archiveDigest, Data: archive}},
	}

	if err := store.PublishSourceRevision(ctx, meta, blobs, req); err != nil {
		t.Fatalf("PublishSourceRevision: %v", err)
	}

	if err := meta.ReadyCheck(ctx, blobs); err != nil {
		t.Fatalf("ReadyCheck after publish: %v", err)
	}

	got, err := meta.GetResourceRevision(ctx, coord, rev.ContentDigest)
	if err != nil {
		t.Fatalf("GetResourceRevision: %v", err)
	}
	if got.ArchiveDigest != rev.ArchiveDigest {
		t.Fatalf("archive digest = %q, want %q", got.ArchiveDigest, rev.ArchiveDigest)
	}

	gotOcc, err := meta.GetSourceOccurrence(ctx, src.ID, occ.Commit)
	if err != nil {
		t.Fatalf("GetSourceOccurrence: %v", err)
	}
	if gotOcc.Commit != occ.Commit {
		t.Fatalf("source occurrence commit = %q, want %q", gotOcc.Commit, occ.Commit)
	}

	blob, err := blobs.Get(ctx, archiveDigest)
	if err != nil {
		t.Fatalf("Get blob: %v", err)
	}
	if string(blob) != string(archive) {
		t.Fatalf("blob content = %q, want %q", blob, archive)
	}
}

func TestPublishSourceRevisionManifestDigestMismatch(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openAtomicStores(t)

	manifest := []byte(`not the expected payload`)
	occ := store.SourceOccurrence{
		SourceID:                     store.SourceID("src-atomic"),
		Ref:                          "main",
		Commit:                       "abc123",
		SourceRevisionManifestDigest: store.RevisionDigest("sha256:expected"),
		Acquisition:                  store.AcquisitionMirror,
		OccurredAt:                   time.Now().UTC(),
	}

	req := store.SourceRevisionPublication{
		Occurrence:      occ,
		ManifestPayload: manifest,
	}

	err := store.PublishSourceRevision(ctx, meta, blobs, req)
	if err == nil {
		t.Fatalf("expected manifest digest mismatch error")
	}
	if !strings.Contains(err.Error(), "manifest payload digest mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublishResourceRevision(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openAtomicStores(t)

	mp := store.Marketplace{
		ID:           store.MarketplaceID("mp-res"),
		Name:         "Resource Marketplace",
		CanonicalURL: "https://res.test",
	}
	if err := meta.PutMarketplace(ctx, mp); err != nil {
		t.Fatalf("PutMarketplace: %v", err)
	}

	src := store.Source{
		ID:          store.SourceID("src-res"),
		Slug:        "res-source",
		Ref:         "main",
		Acquisition: store.AcquisitionMirror,
		Listed:      true,
	}
	if err := meta.PutSource(ctx, src); err != nil {
		t.Fatalf("PutSource: %v", err)
	}

	archive := []byte("resource archive")
	archiveDigest := store.ArchiveDigest("sha256:resarchive")
	rev := store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: mp.ID,
			Source:      src.ID,
			Kind:        "skill",
			Name:        "res",
		},
		ContentDigest: store.RevisionDigest("sha256:rescontent"),
		ArchiveDigest: archiveDigest,
	}

	if err := store.PublishResourceRevision(ctx, meta, blobs, store.ResourceRevisionPublication{
		Revision:     rev,
		ArchiveBytes: archive,
	}); err != nil {
		t.Fatalf("PublishResourceRevision: %v", err)
	}

	if err := meta.ReadyCheck(ctx, blobs); err != nil {
		t.Fatalf("ReadyCheck: %v", err)
	}

	got, err := blobs.Get(ctx, archiveDigest)
	if err != nil {
		t.Fatalf("Get blob: %v", err)
	}
	if string(got) != string(archive) {
		t.Fatalf("blob content = %q, want %q", got, archive)
	}
}
