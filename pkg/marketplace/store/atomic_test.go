package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func sha256Digest(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

func assessmentFor(t *testing.T, rev store.ResourceRevision, inspectionDigest string) store.ResourceAssessment {
	t.Helper()
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: rev.Coordinate, ContentDigest: rev.ContentDigest,
		AnalyzerVersion: "test/v1", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("b", 64)),
		Decision: store.AssessmentDecisionAllow, InspectionManifestDigest: store.RevisionDigest(inspectionDigest), AssessedAt: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return assessment
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

func TestPublishRegistryRevisionStrictlyValidatesInspectionBodies(t *testing.T) {
	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "registry", Kind: "skill", Name: "review"}
	revision := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	body := []byte("# review\\n")
	projection, err := inspection.Build(coordinate, string(revision), body, nil, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	resource := store.ResourceRevision{Coordinate: coordinate, ContentDigest: revision}
	base := store.RegistryRevisionPublication{
		Resources:       []store.ResourceRevision{resource},
		Assessments:     []store.ResourceAssessment{assessmentFor(t, resource, projection.Manifest.ManifestDigest)},
		InspectionBlobs: []store.BlobUpload{{Digest: store.ArchiveDigest(projection.Manifest.ManifestDigest), Data: projection.Payload}, {Digest: store.ArchiveDigest(inspection.Digest(body)), Data: body}},
	}
	for _, tc := range []struct {
		name  string
		build func() store.RegistryRevisionPublication
		want  string
	}{
		{name: "non canonical JSON", build: func() store.RegistryRevisionPublication {
			bad := base
			bad.InspectionBlobs = append([]store.BlobUpload(nil), base.InspectionBlobs...)
			bad.InspectionBlobs[0].Data = append([]byte(" "), bad.InspectionBlobs[0].Data...)
			bad.InspectionBlobs[0].Digest = store.ArchiveDigest(sha256Digest(bad.InspectionBlobs[0].Data))
			bad.Assessments = []store.ResourceAssessment{assessmentFor(t, resource, string(bad.InspectionBlobs[0].Digest))}
			return bad
		}, want: "not canonical JSON"},
		{name: "wrong body size", build: func() store.RegistryRevisionPublication {
			var manifest inspection.Manifest
			if err := json.Unmarshal(projection.Payload, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest.Files[0].Size++
			badPayload, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			badDigest := sha256Digest(badPayload)
			bad := base
			bad.InspectionBlobs = append([]store.BlobUpload(nil), base.InspectionBlobs...)
			bad.InspectionBlobs[0] = store.BlobUpload{Digest: store.ArchiveDigest(badDigest), Data: badPayload}
			bad.Assessments = []store.ResourceAssessment{assessmentFor(t, resource, badDigest)}
			return bad
		}, want: "digest or size"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := store.PublishRegistryRevision(context.Background(), nil, nil, tc.build()); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PublishRegistryRevision error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPublishRegistryRevisionRejectsNonCanonicalInspectionManifestsAndMedia(t *testing.T) {
	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "registry", Kind: "skill", Name: "review"}
	revision := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	projection, err := inspection.Build(coordinate, string(revision), []byte("# review\n"), map[string][]byte{"notes.md": []byte("notes\n")}, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	resource := store.ResourceRevision{Coordinate: coordinate, ContentDigest: revision}
	base := store.RegistryRevisionPublication{Resources: []store.ResourceRevision{resource}, Assessments: []store.ResourceAssessment{assessmentFor(t, resource, projection.Manifest.ManifestDigest)}}
	for digest, data := range projection.Bodies {
		base.InspectionBlobs = append(base.InspectionBlobs, store.BlobUpload{Digest: store.ArchiveDigest(digest), Data: data})
	}
	base.InspectionBlobs = append(base.InspectionBlobs, store.BlobUpload{Digest: store.ArchiveDigest(projection.Manifest.ManifestDigest), Data: projection.Payload})

	mutate := func(t *testing.T, change func(*inspection.Manifest)) store.RegistryRevisionPublication {
		t.Helper()
		var manifest inspection.Manifest
		if err := json.Unmarshal(projection.Payload, &manifest); err != nil {
			t.Fatal(err)
		}
		change(&manifest)
		payload, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		bad := base
		bad.InspectionBlobs = append([]store.BlobUpload(nil), base.InspectionBlobs...)
		bad.InspectionBlobs[len(bad.InspectionBlobs)-1] = store.BlobUpload{Digest: store.ArchiveDigest(sha256Digest(payload)), Data: payload}
		bad.Assessments = []store.ResourceAssessment{assessmentFor(t, resource, sha256Digest(payload))}
		return bad
	}

	cases := []struct {
		name  string
		build func(*testing.T) store.RegistryRevisionPublication
		want  string
	}{
		{name: "unknown field", build: func(t *testing.T) store.RegistryRevisionPublication {
			t.Helper()
			payload := []byte(strings.TrimSuffix(string(projection.Payload), "}") + `,"unknown":true}`)
			return publicationWithInspectionPayload(t, base, payload)
		}, want: "unknown field"},
		{name: "trailing JSON", build: func(t *testing.T) store.RegistryRevisionPublication {
			payload := append(append([]byte(nil), projection.Payload...), []byte("null")...)
			return publicationWithInspectionPayload(t, base, payload)
		}, want: "trailing JSON"},
		{name: "wrong primary media", build: func(t *testing.T) store.RegistryRevisionPublication {
			return mutate(t, func(manifest *inspection.Manifest) { manifest.Files[0].MediaType = inspection.MediaPlain })
		}, want: "primary file media policy"},
		{name: "wrong support media", build: func(t *testing.T) store.RegistryRevisionPublication {
			return mutate(t, func(manifest *inspection.Manifest) { manifest.Files[1].MediaType = inspection.MediaPlain })
		}, want: "media type or inspection class"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := store.PublishRegistryRevision(context.Background(), nil, nil, tc.build(t)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PublishRegistryRevision error = %v, want %q", err, tc.want)
			}
		})
	}
}

func publicationWithInspectionPayload(t *testing.T, base store.RegistryRevisionPublication, payload []byte) store.RegistryRevisionPublication {
	bad := base
	badDigest := sha256Digest(payload)
	bad.InspectionBlobs = append([]store.BlobUpload(nil), base.InspectionBlobs...)
	bad.InspectionBlobs[len(bad.InspectionBlobs)-1] = store.BlobUpload{Digest: store.ArchiveDigest(badDigest), Data: payload}
	bad.Assessments = []store.ResourceAssessment{assessmentFor(t, base.Resources[0], badDigest)}
	return bad
}

func TestPublishRegistryRevisionRejectsInspectionSupportPrimaryAlias(t *testing.T) {
	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "registry", Kind: "skill", Name: "review"}
	revision := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	projection, err := inspection.Build(coordinate, string(revision), []byte("# review\\n"), map[string][]byte{"notes.txt": []byte("notes")}, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	var manifest inspection.Manifest
	if err := json.Unmarshal(projection.Payload, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Files[1].Path = "support/SKILL.md"
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest := store.ArchiveDigest(sha256Digest(payload))
	uploads := []store.BlobUpload{{Digest: manifestDigest, Data: payload}}
	for digest, data := range projection.Bodies {
		uploads = append(uploads, store.BlobUpload{Digest: store.ArchiveDigest(digest), Data: data})
	}
	resource := store.ResourceRevision{Coordinate: coordinate, ContentDigest: revision}
	err = store.PublishRegistryRevision(context.Background(), nil, nil, store.RegistryRevisionPublication{
		Resources:       []store.ResourceRevision{resource},
		Assessments:     []store.ResourceAssessment{assessmentFor(t, resource, string(manifestDigest))},
		InspectionBlobs: uploads,
	})
	if err == nil || !strings.Contains(err.Error(), "non-canonical file path") {
		t.Fatalf("PublishRegistryRevision error = %v, want non-canonical support path rejection", err)
	}
}

func TestPublishRegistryRevision(t *testing.T) {
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

	src := store.Registry{
		ID:          store.RegistryID("src-atomic"),
		Slug:        "atomic-registry",
		Ref:         "main",
		Acquisition: store.AcquisitionMirror,
		Listed:      true,
	}
	if err := meta.PutRegistry(ctx, src); err != nil {
		t.Fatalf("PutRegistry: %v", err)
	}

	manifest := []byte(`{"schema_version":"v1","generator":"test"}`)
	manifestDigest := store.RevisionDigest(sha256Digest(manifest))
	archive := []byte("archive bytes")
	archiveDigest := store.ArchiveDigest(sha256Digest(archive))

	coord := store.ResourceCoordinate{
		Marketplace: mp.ID,
		Registry:    src.ID,
		Kind:        "skill",
		Name:        "member",
	}
	rev := store.ResourceRevision{
		Coordinate:    coord,
		ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
		ArchiveDigest: archiveDigest,
	}
	body := []byte("# member\n")
	projection, err := inspection.Build(coord, string(rev.ContentDigest), body, nil, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	assessment := assessmentFor(t, rev, projection.Manifest.ManifestDigest)

	occ := store.RegistryOccurrence{
		RegistryID:                     src.ID,
		Ref:                            "main",
		Commit:                         "abc123",
		RegistryRevisionManifestDigest: manifestDigest,
		Acquisition:                    store.AcquisitionMirror,
		MirroredArtifactDigest:         archiveDigest,
		OccurredAt:                     time.Now().UTC(),
	}

	req := store.RegistryRevisionPublication{
		Occurrence:          occ,
		Resources:           []store.ResourceRevision{rev},
		ResourceOccurrences: []store.ResourceOccurrence{{Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: occ.Commit, ManifestDigest: manifestDigest}},
		Assessments:         []store.ResourceAssessment{assessment},
		ManifestPayload:     manifest,
		ExtraBlobs:          []store.BlobUpload{{Digest: archiveDigest, Data: archive}},
		InspectionBlobs:     []store.BlobUpload{{Digest: store.ArchiveDigest(projection.Manifest.ManifestDigest), Data: projection.Payload}, {Digest: store.ArchiveDigest(inspection.Digest(body)), Data: body}},
	}

	if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
		t.Fatalf("PublishRegistryRevision: %v", err)
	}

	if err := meta.CheckIntegrity(ctx, blobs); err != nil {
		t.Fatalf("CheckIntegrity after publish: %v", err)
	}

	got, err := meta.GetResourceRevision(ctx, coord, rev.ContentDigest)
	if err != nil {
		t.Fatalf("GetResourceRevision: %v", err)
	}
	if got.ArchiveDigest != rev.ArchiveDigest {
		t.Fatalf("archive digest = %q, want %q", got.ArchiveDigest, rev.ArchiveDigest)
	}

	gotOcc, err := meta.GetRegistryOccurrence(ctx, src.ID, occ.Commit)
	if err != nil {
		t.Fatalf("GetRegistryOccurrence: %v", err)
	}
	if gotOcc.Commit != occ.Commit {
		t.Fatalf("registry occurrence commit = %q, want %q", gotOcc.Commit, occ.Commit)
	}

	blob, err := blobs.Get(ctx, archiveDigest)
	if err != nil {
		t.Fatalf("Get blob: %v", err)
	}
	if string(blob) != string(archive) {
		t.Fatalf("blob content = %q, want %q", blob, archive)
	}
}

func TestPublishRegistryRevisionManifestDigestMismatch(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openAtomicStores(t)

	manifest := []byte(`not the expected payload`)
	occ := store.RegistryOccurrence{
		RegistryID:                     store.RegistryID("src-atomic"),
		Ref:                            "main",
		Commit:                         "abc123",
		RegistryRevisionManifestDigest: store.RevisionDigest("sha256:expected"),
		Acquisition:                    store.AcquisitionMirror,
		OccurredAt:                     time.Now().UTC(),
	}

	req := store.RegistryRevisionPublication{
		Occurrence:      occ,
		ManifestPayload: manifest,
	}

	err := store.PublishRegistryRevision(ctx, meta, blobs, req)
	if err == nil {
		t.Fatalf("expected manifest digest mismatch error")
	}
	if !strings.Contains(err.Error(), "manifest payload digest mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublishRegistryRevisionRejectsCollectionArchive(t *testing.T) {
	err := store.PublishRegistryRevision(context.Background(), nil, nil, store.RegistryRevisionPublication{
		Resources: []store.ResourceRevision{{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "collection", Name: "bundle"},
			ContentDigest: "sha256:collection",
			ArchiveDigest: "sha256:collection-archive",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "collection resource revision") {
		t.Fatalf("expected collection archive rejection, got %v", err)
	}
}

func collectionPublicationFixture(t *testing.T) (store.RegistryRevisionPublication, store.ResourceCoordinate) {
	t.Helper()
	leafData := []byte("leaf archive")
	leafContent := store.RevisionDigest("sha256:" + strings.Repeat("1", 64))
	leafArchive := store.ArchiveDigest(sha256Digest(leafData))
	collectionRegistry := store.RegistryID("src-collection")
	leafRegistry := store.RegistryID("src-leaf")
	manifestPayload, manifestDigest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{
		SchemaVersion:    hccartifact.CollectionSchemaV1,
		Name:             "bundle",
		Members:          []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "member", RegistryID: string(leafRegistry), Revision: string(leafContent)}},
		SupportedSchemas: []string{hccartifact.CollectionSchemaV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	collectionCoord := store.ResourceCoordinate{Marketplace: "mp-collection", Registry: collectionRegistry, Kind: "collection", Name: "bundle"}
	leafCoord := store.ResourceCoordinate{Marketplace: "mp-collection", Registry: leafRegistry, Kind: "skill", Name: "member"}
	collection := store.CollectionRevision{ID: manifestDigest, RegistryID: collectionRegistry, ManifestDigest: store.RevisionDigest(manifestDigest), Members: []store.ResourceCoordinate{leafCoord}}
	registryManifest := []byte("registry revision manifest")
	req := store.RegistryRevisionPublication{
		Occurrence: store.RegistryOccurrence{RegistryID: collectionRegistry, Ref: "main", Commit: "abc123", RegistryRevisionManifestDigest: store.RevisionDigest(sha256Digest(registryManifest)), Acquisition: store.AcquisitionMirror, OccurredAt: time.Now().UTC()},
		Resources: []store.ResourceRevision{
			{Coordinate: leafCoord, ContentDigest: leafContent, ArchiveDigest: leafArchive},
			{Coordinate: collectionCoord, ContentDigest: store.RevisionDigest(manifestDigest)},
		},
		Collections:     []store.CollectionRevision{collection},
		ManifestPayload: registryManifest,
		ExtraBlobs:      []store.BlobUpload{{Digest: store.ArchiveDigest(manifestDigest), Data: manifestPayload}, {Digest: leafArchive, Data: leafData}},
	}
	return req, leafCoord
}

func TestPublishRegistryRevisionRequiresCompleteCollectionGraph(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		mutate func(*store.RegistryRevisionPublication)
		want   string
	}{
		{"missing manifest", func(req *store.RegistryRevisionPublication) { req.ExtraBlobs = req.ExtraBlobs[1:] }, "manifest is missing"},
		{"missing leaf archive", func(req *store.RegistryRevisionPublication) { req.ExtraBlobs = req.ExtraBlobs[:1] }, "archive is missing"},
		{"tampered collection manifest", func(req *store.RegistryRevisionPublication) { req.ExtraBlobs[0].Data = []byte("{}") }, "collection"},
		{"tampered leaf archive", func(req *store.RegistryRevisionPublication) { req.ExtraBlobs[1].Data = []byte("tampered") }, "data digest mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta, blobs := openAtomicStores(t)
			req, leafCoord := collectionPublicationFixture(t)
			tc.mutate(&req)
			if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PublishRegistryRevision error = %v, want %q", err, tc.want)
			}
			if _, err := meta.GetResourceRevision(ctx, leafCoord, req.Resources[0].ContentDigest); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("failed publication committed leaf metadata: %v", err)
			}
		})
	}
}

func TestPublishRegistryRevisionRejectsStaleOrExtraFlattenedLeaves(t *testing.T) {
	for _, tc := range []struct {
		name   string
		leaves func([]hccartifact.CollectionMemberEntry) []hccartifact.CollectionMemberEntry
		want   string
	}{
		{
			name: "stale leaf",
			leaves: func(members []hccartifact.CollectionMemberEntry) []hccartifact.CollectionMemberEntry {
				return []hccartifact.CollectionMemberEntry{{Kind: "skill", Name: "stale", RegistryID: members[0].RegistryID, Revision: members[0].Revision, InclusionPaths: [][]string{{"bundle"}}}}
			},
			want: "missing",
		},
		{
			name: "extra leaf",
			leaves: func(members []hccartifact.CollectionMemberEntry) []hccartifact.CollectionMemberEntry {
				return append(append([]hccartifact.CollectionMemberEntry(nil), members...), hccartifact.CollectionMemberEntry{Kind: "skill", Name: "extra", RegistryID: members[0].RegistryID, Revision: members[0].Revision, InclusionPaths: [][]string{{"bundle"}}})
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
			if err := store.PublishRegistryRevision(context.Background(), meta, blobs, req); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PublishRegistryRevision error = %v, want %q", err, tc.want)
			}
			if _, err := meta.GetResourceRevision(context.Background(), leafCoord, req.Resources[0].ContentDigest); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("invalid flattened leaves committed leaf metadata: %v", err)
			}
		})
	}
}

func TestPublishRegistryRevisionAllowsIndexOnlyLeafAlongsideMirrorCollection(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openAtomicStores(t)
	req, _ := collectionPublicationFixture(t)
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
	if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
		t.Fatalf("mixed mirror/index-only publication failed: %v", err)
	}
}

func TestPublishRegistryRevisionIsIdempotentForCollection(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openAtomicStores(t)
	req, _ := collectionPublicationFixture(t)
	if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
		t.Fatalf("first PublishRegistryRevision: %v", err)
	}
	if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
		t.Fatalf("retry PublishRegistryRevision: %v", err)
	}
	changed := req
	changed.Collections = append([]store.CollectionRevision(nil), req.Collections...)
	changed.Collections[0].Members = nil
	if err := store.PublishRegistryRevision(ctx, meta, blobs, changed); !errors.Is(err, store.ErrConflict) {
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

	src := store.Registry{
		ID:          store.RegistryID("src-res"),
		Slug:        "res-registry",
		Ref:         "main",
		Acquisition: store.AcquisitionMirror,
		Listed:      true,
	}
	if err := meta.PutRegistry(ctx, src); err != nil {
		t.Fatalf("PutRegistry: %v", err)
	}

	archive := []byte("resource archive")
	archiveDigest := store.ArchiveDigest("sha256:resarchive")
	rev := store.ResourceRevision{
		Coordinate: store.ResourceCoordinate{
			Marketplace: mp.ID,
			Registry:    src.ID,
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

	if err := meta.CheckIntegrity(ctx, blobs); err != nil {
		t.Fatalf("CheckIntegrity: %v", err)
	}

	got, err := blobs.Get(ctx, archiveDigest)
	if err != nil {
		t.Fatalf("Get blob: %v", err)
	}
	if string(got) != string(archive) {
		t.Fatalf("blob content = %q, want %q", got, archive)
	}
}

func auditFor(t *testing.T, coord store.ResourceCoordinate, analyzerID, analyzerVersion, configDigest string) store.ResourceAudit {
	t.Helper()
	key := store.ResourceAuditKey{
		Coordinate:      coord,
		SubjectDigest:   store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
		AnalyzerID:      analyzerID,
		AnalyzerVersion: analyzerVersion,
		ConfigDigest:    store.PolicyDigest(configDigest),
	}
	audit, err := store.FinalizeResourceAudit(store.ResourceAudit{
		ResourceAuditKey: key,
		Role:             analyze.RoleGating,
		Verdict:          analyze.VerdictPass,
		Summary:          "ok",
		Duration:         1,
		AssessedAt:       time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("FinalizeResourceAudit: %v", err)
	}
	return audit
}

func TestPublishRegistryRevisionAuditsAndLinks(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openAtomicStores(t)

	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	rev := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64))}
	body := []byte("# review\n")
	projection, err := inspection.Build(coord, string(rev.ContentDigest), body, nil, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	assessment := assessmentFor(t, rev, projection.Manifest.ManifestDigest)
	audit := auditFor(t, coord, "marketplace.hidden-content", "1.2.0", "sha256:"+strings.Repeat("c", 64))

	manifestPayload := []byte(`{"schema_version":"v1","generator":"test"}`)
	manifestDigest := sha256Digest(manifestPayload)
	req := store.RegistryRevisionPublication{
		Occurrence: store.RegistryOccurrence{
			RegistryID:                     "src",
			Ref:                            "main",
			Commit:                         "abc123",
			RegistryRevisionManifestDigest: store.RevisionDigest(manifestDigest),
			Acquisition:                    store.AcquisitionMirror,
			OccurredAt:                     time.Now().UTC(),
		},
		Resources:           []store.ResourceRevision{rev},
		ResourceOccurrences: []store.ResourceOccurrence{{Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "abc123", ManifestDigest: store.RevisionDigest(manifestDigest)}},
		Assessments:         []store.ResourceAssessment{assessment},
		Audits:              []store.ResourceAudit{audit},
		AssessmentAuditLinks: []store.AssessmentAuditLink{{
			AssessmentDigest: assessment.AssessmentDigest,
			Ordinal:          0,
			AuditKey:         audit.ResourceAuditKey,
			ResultDigest:     audit.ResultDigest,
		}},
		ManifestPayload: manifestPayload,
		InspectionBlobs: []store.BlobUpload{
			{Digest: store.ArchiveDigest(projection.Manifest.ManifestDigest), Data: projection.Payload},
			{Digest: store.ArchiveDigest(inspection.Digest(body)), Data: body},
		},
	}

	if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err != nil {
		t.Fatalf("PublishRegistryRevision: %v", err)
	}

	got, err := meta.ListResourceAuditsForAssessment(ctx, assessment.AssessmentDigest)
	if err != nil {
		t.Fatalf("ListResourceAuditsForAssessment: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 audit for assessment, got %d", len(got))
	}
	if got[0].AnalyzerID != audit.AnalyzerID {
		t.Fatalf("audit analyzer = %q, want %q", got[0].AnalyzerID, audit.AnalyzerID)
	}
}

func TestPublishRegistryRevisionRejectsInvalidAuditLinks(t *testing.T) {
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	otherCoord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "other"}
	rev := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64))}
	otherRev := store.ResourceRevision{Coordinate: otherCoord, ContentDigest: rev.ContentDigest}
	assessment := assessmentFor(t, rev, "sha256:"+strings.Repeat("0", 64))
	audit := auditFor(t, coord, "marketplace.hidden-content", "1.2.0", "sha256:"+strings.Repeat("c", 64))
	audit2 := auditFor(t, coord, "marketplace.assets", "1.2.0", "sha256:"+strings.Repeat("d", 64))

	cases := []struct {
		name string
		req  store.RegistryRevisionPublication
		want string
	}{
		{
			name: "audit references unknown resource",
			req: store.RegistryRevisionPublication{
				Resources: []store.ResourceRevision{rev},
				Audits:    []store.ResourceAudit{auditFor(t, otherCoord, "marketplace.hidden-content", "1.2.0", "sha256:"+strings.Repeat("c", 64))},
			},
			want: "unknown resource coordinate",
		},
		{
			name: "duplicate audit",
			req: store.RegistryRevisionPublication{
				Resources: []store.ResourceRevision{rev},
				Audits:    []store.ResourceAudit{audit, audit},
			},
			want: "duplicate audit",
		},
		{
			name: "link references unknown audit",
			req: store.RegistryRevisionPublication{
				Resources:   []store.ResourceRevision{rev},
				Assessments: []store.ResourceAssessment{assessment},
				Audits:      []store.ResourceAudit{audit},
				AssessmentAuditLinks: []store.AssessmentAuditLink{{
					AssessmentDigest: assessment.AssessmentDigest,
					Ordinal:          0,
					AuditKey: store.ResourceAuditKey{
						Coordinate:      coord,
						SubjectDigest:   store.RevisionDigest("sha256:" + strings.Repeat("2", 64)),
						AnalyzerID:      "marketplace.missing",
						AnalyzerVersion: "1.2.0",
						ConfigDigest:    store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
					},
					ResultDigest: audit.ResultDigest,
				}},
			},
			want: "unknown audit",
		},
		{
			name: "link result digest mismatch",
			req: store.RegistryRevisionPublication{
				Resources:   []store.ResourceRevision{rev},
				Assessments: []store.ResourceAssessment{assessment},
				Audits:      []store.ResourceAudit{audit},
				AssessmentAuditLinks: []store.AssessmentAuditLink{{
					AssessmentDigest: assessment.AssessmentDigest,
					Ordinal:          0,
					AuditKey:         audit.ResourceAuditKey,
					ResultDigest:     store.AuditDigest("sha256:" + strings.Repeat("4", 64)),
				}},
			},
			want: "result digest mismatch",
		},
		{
			name: "link references unknown assessment",
			req: store.RegistryRevisionPublication{
				Resources: []store.ResourceRevision{rev},
				Audits:    []store.ResourceAudit{audit},
				AssessmentAuditLinks: []store.AssessmentAuditLink{{
					AssessmentDigest: store.AssessmentDigest("sha256:" + strings.Repeat("3", 64)),
					Ordinal:          0,
					AuditKey:         audit.ResourceAuditKey,
					ResultDigest:     audit.ResultDigest,
				}},
			},
			want: "unknown assessment",
		},
		{
			name: "cross-assessment coordinate",
			req: store.RegistryRevisionPublication{
				Resources:   []store.ResourceRevision{rev, otherRev},
				Assessments: []store.ResourceAssessment{assessment},
				Audits:      []store.ResourceAudit{auditFor(t, otherCoord, "marketplace.hidden-content", "1.2.0", "sha256:"+strings.Repeat("c", 64))},
				AssessmentAuditLinks: []store.AssessmentAuditLink{{
					AssessmentDigest: assessment.AssessmentDigest,
					Ordinal:          0,
					AuditKey:         auditFor(t, otherCoord, "marketplace.hidden-content", "1.2.0", "sha256:"+strings.Repeat("c", 64)).ResourceAuditKey,
					ResultDigest:     auditFor(t, otherCoord, "marketplace.hidden-content", "1.2.0", "sha256:"+strings.Repeat("c", 64)).ResultDigest,
				}},
			},
			want: "cross-assessment coordinate",
		},
		{
			name: "duplicate ordinal",
			req: store.RegistryRevisionPublication{
				Resources:   []store.ResourceRevision{rev},
				Assessments: []store.ResourceAssessment{assessment},
				Audits:      []store.ResourceAudit{audit, audit2},
				AssessmentAuditLinks: []store.AssessmentAuditLink{
					{AssessmentDigest: assessment.AssessmentDigest, Ordinal: 0, AuditKey: audit.ResourceAuditKey, ResultDigest: audit.ResultDigest},
					{AssessmentDigest: assessment.AssessmentDigest, Ordinal: 0, AuditKey: audit2.ResourceAuditKey, ResultDigest: audit2.ResultDigest},
				},
			},
			want: "duplicate ordinal",
		},
		{
			name: "noncontiguous ordinals",
			req: store.RegistryRevisionPublication{
				Resources:   []store.ResourceRevision{rev},
				Assessments: []store.ResourceAssessment{assessment},
				Audits:      []store.ResourceAudit{audit, audit2},
				AssessmentAuditLinks: []store.AssessmentAuditLink{
					{AssessmentDigest: assessment.AssessmentDigest, Ordinal: 0, AuditKey: audit.ResourceAuditKey, ResultDigest: audit.ResultDigest},
					{AssessmentDigest: assessment.AssessmentDigest, Ordinal: 2, AuditKey: audit2.ResourceAuditKey, ResultDigest: audit2.ResultDigest},
				},
			},
			want: "noncontiguous",
		},
		{
			name: "duplicate assessment digest",
			req: store.RegistryRevisionPublication{
				Resources:   []store.ResourceRevision{rev},
				Assessments: []store.ResourceAssessment{assessment, assessment},
			},
			want: "duplicate assessment digest",
		},
		{
			name: "duplicate analyzer id within assessment",
			req: store.RegistryRevisionPublication{
				Resources:   []store.ResourceRevision{rev},
				Assessments: []store.ResourceAssessment{assessment},
				Audits:      []store.ResourceAudit{audit, auditFor(t, coord, "marketplace.hidden-content", "2.0.0", "sha256:"+strings.Repeat("e", 64))},
				AssessmentAuditLinks: []store.AssessmentAuditLink{
					{AssessmentDigest: assessment.AssessmentDigest, Ordinal: 0, AuditKey: audit.ResourceAuditKey, ResultDigest: audit.ResultDigest},
					{AssessmentDigest: assessment.AssessmentDigest, Ordinal: 1, AuditKey: auditFor(t, coord, "marketplace.hidden-content", "2.0.0", "sha256:"+strings.Repeat("e", 64)).ResourceAuditKey, ResultDigest: auditFor(t, coord, "marketplace.hidden-content", "2.0.0", "sha256:"+strings.Repeat("e", 64)).ResultDigest},
				},
			},
			want: "duplicate analyzer id",
		},
		{
			name: "unreferenced request audit",
			req: store.RegistryRevisionPublication{
				Resources:   []store.ResourceRevision{rev},
				Assessments: []store.ResourceAssessment{assessment},
				Audits:      []store.ResourceAudit{audit, audit2},
				AssessmentAuditLinks: []store.AssessmentAuditLink{
					{AssessmentDigest: assessment.AssessmentDigest, Ordinal: 0, AuditKey: audit.ResourceAuditKey, ResultDigest: audit.ResultDigest},
				},
			},
			want: "not referenced",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := store.PublishRegistryRevision(context.Background(), nil, nil, tc.req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PublishRegistryRevision error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPublishRegistryRevisionRollsBackOnInvalidAssessment(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openAtomicStores(t)

	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	rev := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64))}
	audit := auditFor(t, coord, "marketplace.hidden-content", "1.2.0", "sha256:"+strings.Repeat("c", 64))
	badCoord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "missing"}
	badAssessment := assessmentFor(t, store.ResourceRevision{Coordinate: badCoord, ContentDigest: rev.ContentDigest}, "sha256:"+strings.Repeat("0", 64))

	req := store.RegistryRevisionPublication{
		Occurrence: store.RegistryOccurrence{
			RegistryID:                     "src",
			Ref:                            "main",
			Commit:                         "abc123",
			RegistryRevisionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("5", 64)),
			Acquisition:                    store.AcquisitionMirror,
			OccurredAt:                     time.Now().UTC(),
		},
		Resources:           []store.ResourceRevision{rev},
		ResourceOccurrences: []store.ResourceOccurrence{{Coordinate: coord, ContentDigest: rev.ContentDigest, Commit: "abc123", ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("5", 64))}},
		Assessments:         []store.ResourceAssessment{badAssessment},
		Audits:              []store.ResourceAudit{audit},
	}

	if err := store.PublishRegistryRevision(ctx, meta, blobs, req); err == nil {
		t.Fatalf("expected publish to fail")
	}

	if _, err := meta.GetResourceRevision(ctx, coord, rev.ContentDigest); err == nil {
		t.Fatalf("expected resource revision to be rolled back")
	}
	if _, err := meta.GetResourceAudit(ctx, audit.ResourceAuditKey, audit.ResultDigest); err == nil {
		t.Fatalf("expected audit to be rolled back")
	}
}

func TestPublishRegistryRevisionAuditValidationLeavesStateUnchanged(t *testing.T) {
	ctx := context.Background()
	meta, blobs := openAtomicStores(t)

	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	rev := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64))}
	assessment := assessmentFor(t, rev, "sha256:"+strings.Repeat("0", 64))
	audit := auditFor(t, coord, "marketplace.hidden-content", "1.2.0", "sha256:"+strings.Repeat("c", 64))
	audit2 := auditFor(t, coord, "marketplace.assets", "1.2.0", "sha256:"+strings.Repeat("d", 64))
	manifestDigest := store.ArchiveDigest("sha256:" + strings.Repeat("5", 64))

	badReq := store.RegistryRevisionPublication{
		Occurrence: store.RegistryOccurrence{
			RegistryID:                     "src",
			Ref:                            "main",
			Commit:                         "abc123",
			RegistryRevisionManifestDigest: store.RevisionDigest(manifestDigest),
			Acquisition:                    store.AcquisitionMirror,
			OccurredAt:                     time.Now().UTC(),
		},
		Resources:   []store.ResourceRevision{rev},
		Assessments: []store.ResourceAssessment{assessment},
		Audits:      []store.ResourceAudit{audit, audit2},
		AssessmentAuditLinks: []store.AssessmentAuditLink{
			{AssessmentDigest: assessment.AssessmentDigest, Ordinal: 0, AuditKey: audit.ResourceAuditKey, ResultDigest: audit.ResultDigest},
		},
		ManifestPayload: []byte(`{"schema_version":"v1","generator":"test"}`),
	}

	if err := store.PublishRegistryRevision(ctx, meta, blobs, badReq); err == nil {
		t.Fatalf("expected publish to fail")
	}

	if _, err := meta.GetResourceRevision(ctx, coord, rev.ContentDigest); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected no resource revision after failed publish, got err=%v", err)
	}
	if _, err := meta.GetResourceAssessment(ctx, assessment.AssessmentDigest); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected no assessment after failed publish, got err=%v", err)
	}
	if _, err := meta.GetResourceAudit(ctx, audit.ResourceAuditKey, audit.ResultDigest); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected no audit after failed publish, got err=%v", err)
	}
	if _, err := blobs.Get(ctx, manifestDigest); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected no blob upload after failed publish, got err=%v", err)
	}
}
