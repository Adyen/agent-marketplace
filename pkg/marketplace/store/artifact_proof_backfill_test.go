package store_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
)

func artifactProofBackfillRequest(after store.ArchiveDigest, limit int) store.ArtifactProofBackfillRequest {
	return store.ArtifactProofBackfillRequest{
		Marketplace: "managed-test",
		After:       after,
		Limit:       limit,
		BlobLimit:   16 << 20,
	}
}

func TestBackfillArtifactProofsIsBoundedAndResumable(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	putBlobOwnershipRegistry(t, meta, "registry")
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))

	var digests []store.ArchiveDigest
	for _, name := range []string{"first", "second"} {
		canonical, err := hccartifact.FromIR(ir.NewSkill(name, "Title", "body", nil, nil, ir.SourceRef{}))
		if err != nil {
			t.Fatal(err)
		}
		canonical.Manifest.RegistryID = "registry"
		canonical.Manifest.Revision = canonical.Manifest.ContentDigest
		raw, _, digest, err := hccartifact.EncodeWithProof(canonical)
		if err != nil {
			t.Fatal(err)
		}
		archiveDigest := store.ArchiveDigest(digest)
		if err := blobs.Put(ctx, archiveDigest, raw); err != nil {
			t.Fatal(err)
		}
		if err := meta.PutResourceRevision(ctx, store.ResourceRevision{
			Coordinate: store.ResourceCoordinate{
				Marketplace: "managed-test",
				Registry:    "registry",
				Kind:        "skill",
				Name:        name,
			},
			ContentDigest: store.RevisionDigest(canonical.Manifest.ContentDigest),
			ArchiveDigest: archiveDigest,
		}); err != nil {
			t.Fatal(err)
		}
		digests = append(digests, archiveDigest)
	}

	first, err := store.BackfillArtifactProofs(ctx, meta, blobs, artifactProofBackfillRequest("", 1))
	if err != nil {
		t.Fatalf("first backfill page: %v", err)
	}
	if first.Processed != 1 || first.Created != 1 || !first.Remaining || first.NextAfter == "" {
		t.Fatalf("first backfill page = %+v", first)
	}
	second, err := store.BackfillArtifactProofs(ctx, meta, blobs, artifactProofBackfillRequest(first.NextAfter, 1))
	if err != nil {
		t.Fatalf("second backfill page: %v", err)
	}
	if second.Processed != 1 || second.Created != 1 || second.Remaining {
		t.Fatalf("second backfill page = %+v", second)
	}

	for _, digest := range digests {
		proof, err := blobs.Get(ctx, store.ArtifactProofBlobKey(digest))
		if err != nil {
			t.Fatalf("get proof for %s: %v", digest, err)
		}
		if _, err := hccartifact.DecodeProof(proof, string(digest), nil); err != nil {
			t.Fatalf("verify proof for %s: %v", digest, err)
		}
	}

	repeated, err := store.BackfillArtifactProofs(ctx, meta, blobs, artifactProofBackfillRequest("", 2))
	if err != nil {
		t.Fatalf("repeat backfill: %v", err)
	}
	if repeated.Processed != 2 || repeated.Created != 0 || repeated.Existing != 2 {
		t.Fatalf("repeat backfill page = %+v", repeated)
	}
}

func TestBackfillArtifactProofsRequiresProducingToolchain(t *testing.T) {
	ctx := context.Background()
	meta, _ := managedStore(t)
	putBlobOwnershipRegistry(t, meta, "registry")
	blobs := fsblob.New(filepath.Join(t.TempDir(), "blobs"))
	raw, err := os.ReadFile(filepath.Join("..", "..", "harness-compilation", "artifact", "testdata", "regression-using-git-worktrees.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	canonical, _, err := hccartifact.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	digest := store.ArchiveDigest(canonical.Manifest.ArchiveDigest)
	if err := blobs.Put(ctx, digest, raw); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutResourceRevision(ctx, store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: "managed-test",
			Registry:    "registry",
			Kind:        canonical.Manifest.Kind,
			Name:        canonical.Manifest.Name,
		},
		ContentDigest: store.RevisionDigest(canonical.Manifest.ContentDigest),
		ArchiveDigest: digest,
	}); err != nil {
		t.Fatal(err)
	}

	result, err := store.BackfillArtifactProofs(ctx, meta, blobs, artifactProofBackfillRequest("", 1))
	switch {
	case strings.HasPrefix(runtime.Version(), "go1.26"):
		if err != nil || result.Created != 1 {
			t.Fatalf("Go 1.26 backfill = %+v, %v", result, err)
		}
	case strings.HasPrefix(runtime.Version(), "go1.27"):
		if !errors.Is(err, store.ErrArtifactProofBackfillIncompatible) {
			t.Fatalf("Go 1.27 backfill error = %v, want incompatible toolchain", err)
		}
	default:
		t.Skipf("backfill compatibility expectation is not defined for %s", runtime.Version())
	}
}
