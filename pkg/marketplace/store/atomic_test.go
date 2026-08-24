package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
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
	archiveDigest := store.ArchiveDigest(sha256Digest(archive))

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

func TestPublishSourceRevisionRejectsCollectionArchive(t *testing.T) {
	err := store.PublishSourceRevision(context.Background(), nil, nil, store.SourceRevisionPublication{
		Resources: []store.ResourceRevision{{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: "collection", Name: "bundle"},
			ContentDigest: "sha256:collection",
			ArchiveDigest: "sha256:collection-archive",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "collection resource revision") {
		t.Fatalf("expected collection archive rejection, got %v", err)
	}
}

func collectionPublicationFixture(t *testing.T) (store.SourceRevisionPublication, store.ResourceCoordinate) {
	t.Helper()
	leafData := []byte("leaf archive")
	leafContent := store.RevisionDigest("sha256:" + strings.Repeat("1", 64))
	leafArchive := store.ArchiveDigest(sha256Digest(leafData))
	collectionSource := store.SourceID("src-collection")
	leafSource := store.SourceID("src-leaf")
	manifestPayload, manifestDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion:    hccartifact.CollectionSchemaV1,
		Name:             "bundle",
		Members:          []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "member", SourceID: string(leafSource), Revision: string(leafContent)}},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	collectionCoord := store.ResourceCoordinate{Marketplace: "mp-collection", Source: collectionSource, Kind: "collection", Name: "bundle"}
	leafCoord := store.ResourceCoordinate{Marketplace: "mp-collection", Source: leafSource, Kind: "skill", Name: "member"}
	collection := store.CollectionRevision{ID: manifestDigest, SourceID: collectionSource, ManifestDigest: store.RevisionDigest(manifestDigest), Members: []store.ResourceCoordinate{leafCoord}}
	sourceManifest := []byte("source revision manifest")
	req := store.SourceRevisionPublication{
		Occurrence: store.SourceOccurrence{SourceID: collectionSource, Ref: "main", Commit: "abc123", SourceRevisionManifestDigest: store.RevisionDigest(sha256Digest(sourceManifest)), Acquisition: store.AcquisitionMirror, OccurredAt: time.Now().UTC()},
		Resources: []store.ResourceRevision{
			{Coordinate: leafCoord, ContentDigest: leafContent, ArchiveDigest: leafArchive},
			{Coordinate: collectionCoord, ContentDigest: store.RevisionDigest(manifestDigest)},
		},
		Collections:     []store.CollectionRevision{collection},
		ManifestPayload: sourceManifest,
		ExtraBlobs:      []store.BlobUpload{{Digest: store.ArchiveDigest(manifestDigest), Data: manifestPayload}, {Digest: leafArchive, Data: leafData}},
	}
	return req, leafCoord
}

func TestPublishSourceRevisionRequiresCompleteCollectionGraph(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		mutate func(*store.SourceRevisionPublication)
		want   string
	}{
		{"missing manifest", func(req *store.SourceRevisionPublication) { req.ExtraBlobs = req.ExtraBlobs[1:] }, "manifest is missing"},
		{"missing leaf archive", func(req *store.SourceRevisionPublication) { req.ExtraBlobs = req.ExtraBlobs[:1] }, "archive is missing"},
		{"tampered collection manifest", func(req *store.SourceRevisionPublication) { req.ExtraBlobs[0].Data = []byte("{}") }, "collection"},
		{"tampered leaf archive", func(req *store.SourceRevisionPublication) { req.ExtraBlobs[1].Data = []byte("tampered") }, "data digest mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta, blobs := openAtomicStores(t)
			req, leafCoord := collectionPublicationFixture(t)
			tc.mutate(&req)
			if err := store.PublishSourceRevision(ctx, meta, blobs, req); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PublishSourceRevision error = %v, want %q", err, tc.want)
			}
			if _, err := meta.GetResourceRevision(ctx, leafCoord, req.Resources[0].ContentDigest); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("failed publication committed leaf metadata: %v", err)
			}
		})
	}
}

func TestPublishSourceRevisionRejectsStaleOrExtraFlattenedLeaves(t *testing.T) {
	for _, tc := range []struct {
		name   string
		leaves func([]hccartifact.CollectionMemberEntry) []hccartifact.CollectionMemberEntry
		want   string
	}{
		{
			name: "stale leaf",
			leaves: func(members []hccartifact.CollectionMemberEntry) []hccartifact.CollectionMemberEntry {
				return []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "stale", SourceID: members[0].SourceID, Revision: members[0].Revision, InclusionPaths: [][]string{{"bundle"}}}}
			},
			want: "missing",
		},
		{
			name: "extra leaf",
			leaves: func(members []hccartifact.CollectionMemberEntry) []hccartifact.CollectionMemberEntry {
				return append(append([]hccartifact.CollectionMemberEntry(nil), members...), hccartifact.CollectionMemberEntry{Kind: "skill", Name: "extra", SourceID: members[0].SourceID, Revision: members[0].Revision, InclusionPaths: [][]string{{"bundle"}}})
			},
			want: "extra",
		},
		{
			name: "invalid inclusion path",
			leaves: func(members []hccartifact.CollectionMemberEntry) []hccartifact.CollectionMemberEntry {
				members[0].InclusionPaths = [][]string{{"bundle", "not-a-member"}}
				return members
			},
			want: "inclusion paths",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, leafCoord := collectionPublicationFixture(t)
			manifest, _, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(req.ExtraBlobs[0].Data))
			if err != nil {
				t.Fatal(err)
			}
			members := append([]hccartifact.CollectionMemberEntry(nil), manifest.Members...)
			for i := range members {
				members[i].InclusionPaths = [][]string{{manifest.Name}}
			}
			manifest.Leaves = tc.leaves(members)
			payload, digest, err := hccartifact.EncodeCollectionManifest(manifest)
			if err != nil {
				t.Fatal(err)
			}
			req.ExtraBlobs[0] = store.BlobUpload{Digest: store.ArchiveDigest(digest), Data: payload}
			req.Resources[1].ContentDigest = store.RevisionDigest(digest)
			req.Collections[0].ID = digest
			req.Collections[0].ManifestDigest = store.RevisionDigest(digest)

			meta, blobs := openAtomicStores(t)
			if err := store.PublishSourceRevision(context.Background(), meta, blobs, req); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PublishSourceRevision error = %v, want %q", err, tc.want)
			}
			if _, err := meta.GetResourceRevision(context.Background(), leafCoord, req.Resources[0].ContentDigest); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("invalid flattened leaves committed leaf metadata: %v", err)
			}
		})
	}
}

func TestPublishSourceRevisionAllowsIndexOnlyLeafAlongsideMirrorCollection(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openAtomicStores(t)
	req, leafCoord := collectionPublicationFixture(t)
	manifest, _, err := hccartifact.DecodeCollectionManifest(strings.NewReader(string(req.ExtraBlobs[0].Data)))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Leaves = append([]hccartifact.CollectionMemberEntry(nil), manifest.Members...)
	manifest.Leaves[0].InclusionPaths = [][]string{{manifest.Name}}
	payload, digest, err := hccartifact.EncodeCollectionManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	req.ExtraBlobs[0] = store.BlobUpload{Digest: store.ArchiveDigest(digest), Data: payload}
	req.Resources[0].ArchiveDigest = ""
	req.Resources[1].ContentDigest = store.RevisionDigest(digest)
	req.Collections[0].ID = digest
	req.Collections[0].ManifestDigest = store.RevisionDigest(digest)
	req.Resources[0].Provenance = &store.Provenance{SourceID: leafCoord.Source, Commit: "abc123", ManifestDigest: req.Occurrence.SourceRevisionManifestDigest}
	if err := store.PublishSourceRevision(ctx, meta, blobs, req); err != nil {
		t.Fatalf("mixed mirror/index-only publication failed: %v", err)
	}
}

func TestPublishSourceRevisionIsIdempotentForCollection(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openAtomicStores(t)
	req, _ := collectionPublicationFixture(t)
	if err := store.PublishSourceRevision(ctx, meta, blobs, req); err != nil {
		t.Fatalf("first PublishSourceRevision: %v", err)
	}
	if err := store.PublishSourceRevision(ctx, meta, blobs, req); err != nil {
		t.Fatalf("retry PublishSourceRevision: %v", err)
	}
	changed := req
	changed.Collections = append([]store.CollectionRevision(nil), req.Collections...)
	changed.Collections[0].Members = nil
	if err := store.PublishSourceRevision(ctx, meta, blobs, changed); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("conflicting collection metadata error = %v, want ErrConflict", err)
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
