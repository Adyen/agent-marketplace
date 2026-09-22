package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestResourceArtifactConformance(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openStores(t)
	associations := meta.(store.ResourceArtifactStore)
	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "reg", Kind: "skill", Name: "review"}
	payload := []byte(`{}`)
	digest := store.RevisionDigest(artifact.ContentDigest(payload))
	source := artifact.ArtifactV2{Manifest: artifact.ManifestV2{SchemaVersion: "v2", ArtifactVersion: "v2", Kind: "skill", Name: "review", ContentDigest: string(digest), Provenance: artifact.ProvenanceV2{Generator: artifact.GeneratorV2}, SupportedSchemas: []string{"v2"}}, Payload: payload}
	encoded, err := artifact.EncodeV2(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	record := store.ResourceArtifact{Coordinate: coordinate, ContentDigest: digest, Reference: encoded.Reference}
	if err := associations.PutResourceArtifact(ctx, record); err == nil {
		t.Fatal("accepted orphan association")
	}
	if err := meta.PutResourceRevision(ctx, store.ResourceRevision{Coordinate: coordinate, ContentDigest: digest}); err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback")
	if err := meta.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		if err := tx.(store.ResourceArtifactStore).PutResourceArtifact(ctx, record); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	records, err := associations.ListResourceArtifacts(ctx, coordinate, digest)
	if err != nil || len(records) != 0 {
		t.Fatalf("rollback: %v %v", records, err)
	}
	if err := associations.PutResourceArtifact(ctx, record); err != nil {
		t.Fatal(err)
	}
	records, err = associations.ListResourceArtifacts(ctx, coordinate, digest)
	if err != nil || len(records) != 1 || records[0].Reference != record.Reference || records[0].CreatedAt.IsZero() {
		t.Fatalf("stored: %v %v", records, err)
	}
	created := records[0].CreatedAt
	if err := associations.PutResourceArtifact(ctx, record); err != nil {
		t.Fatal(err)
	}
	records, err = associations.ListResourceArtifacts(ctx, coordinate, digest)
	if err != nil || !records[0].CreatedAt.Equal(created) {
		t.Fatal("idempotent write changed creation time", err)
	}
	conflict := record
	conflict.Reference.BlobDigest = artifact.BlobDigest("sha256:" + strings.Repeat("a", 64))
	if err := associations.PutResourceArtifact(ctx, conflict); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("conflict: %v", err)
	}
	owners, err := associations.ListResourceArtifactsByBlob(ctx, "mp", record.Reference.BlobDigest)
	if err != nil || len(owners) != 1 || owners[0].Coordinate != coordinate {
		t.Fatalf("owners: %v %v", owners, err)
	}
	owners, err = associations.ListResourceArtifactsByBlob(ctx, "other", record.Reference.BlobDigest)
	if err != nil || len(owners) != 0 {
		t.Fatalf("cross-marketplace owners: %v %v", owners, err)
	}
	if err := meta.CheckIntegrity(ctx, blobs); !errors.Is(err, store.ErrBlobMissing) {
		t.Fatalf("missing blob: %v", err)
	}
	if err := blobs.Put(ctx, store.ArchiveDigest(record.Reference.BlobDigest), encoded.Bytes); err != nil {
		t.Fatal(err)
	}
	if err := meta.CheckIntegrity(ctx, blobs); err != nil {
		t.Fatal(err)
	}
}
