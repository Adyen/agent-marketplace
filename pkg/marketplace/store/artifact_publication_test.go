package store_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestArtifactV2PublicationAndBackfill(t *testing.T) {
	for _, backfill := range []bool{false, true} {
		t.Run(map[bool]string{false: "dual", true: "backfill"}[backfill], func(t *testing.T) {
			ctx := context.Background()
			meta, blobs := openStores(t)
			req := ingestedRootCollection(t, meta)
			var err error
			if !backfill {
				req, err = store.PrepareArtifactV2Publication(ctx, meta, blobs, req)
				if err != nil {
					t.Fatal(err)
				}
				if len(req.Artifacts) != 1 {
					t.Fatal("expected one v2 resource", len(req.Artifacts))
				}
			}
			if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
				t.Fatal(err)
			}
			var rev store.ResourceRevision
			for _, r := range req.Resources {
				if r.Coordinate.Kind == "skill" {
					rev = r
				}
			}
			before, err := meta.GetResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
			if err != nil {
				t.Fatal(err)
			}
			if backfill {
				if _, err := store.BackfillResourceArtifact(ctx, meta, blobs, rev.Coordinate, rev.ContentDigest); err != nil {
					t.Fatal(err)
				}
			}
			associations := meta.(store.ResourceArtifactStore)
			records, err := associations.ListResourceArtifacts(ctx, rev.Coordinate, rev.ContentDigest)
			if err != nil || len(records) != 1 {
				t.Fatalf("records %v %v", records, err)
			}
			after, err := meta.GetResourceRevision(ctx, rev.Coordinate, rev.ContentDigest)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("backfill changed revision: %v", err)
			}
			original := records[0]
			if _, err := store.BackfillResourceArtifact(ctx, meta, blobs, rev.Coordinate, rev.ContentDigest); err != nil {
				t.Fatal(err)
			}
			records, err = associations.ListResourceArtifacts(ctx, rev.Coordinate, rev.ContentDigest)
			if err != nil || !reflect.DeepEqual(records[0], original) {
				t.Fatalf("republication changed association: %v", err)
			}
			if err := meta.CheckIntegrity(ctx, blobs); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestArtifactPublicationRejectsProtectedEntryMismatch(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openStores(t)
	req := ingestedRootCollection(t, meta)
	req, err := store.PrepareArtifactV2Publication(ctx, meta, blobs, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Artifacts) != 1 {
		t.Fatal("missing positive-control artifact")
	}
	record := req.Artifacts[0]
	for i, b := range req.ExtraBlobs {
		if b.Digest != store.ArchiveDigest(record.Reference.BlobDigest) {
			continue
		}
		decoded, err := artifact.VerifyV2(b.Data, record.Reference, nil)
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Assets == nil {
			decoded.Assets = make(map[string][]byte)
		}
		decoded.Assets["new.txt"] = []byte("changed protected entries")
		changed, err := artifact.EncodeV2(decoded, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Artifacts[0].Reference = changed.Reference
		req.ExtraBlobs[i] = store.BlobUpload{Digest: store.ArchiveDigest(changed.Reference.BlobDigest), Data: changed.Bytes}
	}
	if err := store.PublishRegistryRevision(ctx, meta, blobs, req); !errors.Is(err, artifact.ErrProtectedEntryMismatch) {
		t.Fatalf("wrong failure: %v", err)
	}
	records, err := meta.(store.ResourceArtifactStore).ListResourceArtifacts(ctx, record.Coordinate, record.ContentDigest)
	if err != nil || len(records) != 0 {
		t.Fatalf("published invalid association: %v %v", records, err)
	}
	if _, err := meta.GetRegistryOccurrence(ctx, req.Occurrence.RegistryID, req.Occurrence.Commit); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("published occurrence: %v", err)
	}
	exists, err := blobs.Exists(ctx, store.ArchiveDigest(req.Artifacts[0].Reference.BlobDigest))
	if err != nil || exists {
		t.Fatalf("uploaded invalid candidate: %v", err)
	}
}
