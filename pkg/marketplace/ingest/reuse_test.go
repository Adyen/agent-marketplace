package ingest

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// reuseMeta answers only the stored-revision lookup that archive reuse performs.
type reuseMeta struct {
	store.MetadataStore
	stored store.ResourceRevision
}

func (m reuseMeta) GetResourceRevision(_ context.Context, coord store.ResourceCoordinate, digest store.RevisionDigest) (store.AssessedResourceRevision, error) {
	if coord != m.stored.Coordinate || digest != m.stored.ContentDigest {
		return store.AssessedResourceRevision{}, store.ErrNotFound
	}
	return store.AssessedResourceRevision{ResourceRevision: m.stored}, nil
}

// reuseBlobs is an in-memory blob store that answers only Get.
type reuseBlobs struct {
	store.BlobStore
	data map[store.ArchiveDigest][]byte
}

func (b reuseBlobs) Get(_ context.Context, digest store.ArchiveDigest) ([]byte, error) {
	data, ok := b.data[digest]
	if !ok {
		return nil, store.ErrNotFound
	}
	return data, nil
}

// reuseFixture is an mcpserver archive with its final and placeholder forms.
type reuseFixture struct {
	final, placeholder []byte
	digest             store.ArchiveDigest
	artifact           hccartifact.CanonicalArtifact
}

func readReuseFixture(t *testing.T, version string) reuseFixture {
	t.Helper()
	dir := filepath.Join("..", "..", "harness-compilation", "artifact", "testdata", "toolchain", version)
	final, err := os.ReadFile(filepath.Join(dir, "mcpserver.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	placeholder, err := os.ReadFile(filepath.Join(dir, "mcpserver.placeholder.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := hccartifact.Decode(bytes.NewReader(final))
	if err != nil {
		t.Fatal(err)
	}
	return reuseFixture{final: final, placeholder: placeholder, digest: store.ArchiveDigest(a.Manifest.ArchiveDigest), artifact: a}
}

// encodeReuseArchives encodes a with the running toolchain.
func encodeReuseArchives(t *testing.T, a hccartifact.CanonicalArtifact) reuseFixture {
	t.Helper()
	final, placeholder, digest, err := hccartifact.EncodeArchives(a)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := hccartifact.Decode(bytes.NewReader(final))
	if err != nil {
		t.Fatal(err)
	}
	return reuseFixture{final: final, placeholder: placeholder, digest: store.ArchiveDigest(digest), artifact: decoded}
}

// revisionBuiltFrom returns a revision that carries the built archive as a mirror publication would.
func revisionBuiltFrom(t *testing.T, built reuseFixture, contentDigest store.RevisionDigest) *Revision {
	t.Helper()
	coord := store.ResourceCoordinate{Marketplace: "mp-test", Registry: "src-test", Kind: "mcpserver", Name: "genai-docs"}
	payload, digest, err := artifact.EncodeRegistryRevisionManifest(artifact.RegistryRevisionManifest{
		SchemaVersion: artifact.RegistryRevisionSchemaV1,
		Generator:     artifact.RegistryRevisionGeneratorV1,
		Resources: []artifact.RevisionResource{{
			Kind: "mcpserver", Name: "genai-docs", ContentDigest: string(contentDigest), ArchiveDigest: string(built.digest),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &Revision{
		Occurrence:          store.RegistryOccurrence{RegistryID: "src-test", RegistryRevisionManifestDigest: store.RevisionDigest(digest)},
		Resources:           []store.ResourceRevision{{Coordinate: coord, ContentDigest: contentDigest, ArchiveDigest: built.digest}},
		ResourceOccurrences: []store.ResourceOccurrence{{Coordinate: coord, ContentDigest: contentDigest, ManifestDigest: store.RevisionDigest(digest)}},
		ExtraBlobs: []store.BlobUpload{
			{Digest: built.digest, Data: built.final},
			{Digest: store.PlaceholderArchiveKey(built.digest), Data: built.placeholder},
		},
		ManifestPayload: payload,
		ManifestDigest:  store.RevisionDigest(digest),
	}
}

// spec: marketplace-publication/identical-content-republished-under-another-toolchain-keeps-its-archive-digest
func TestReuseStoredArchivesKeepsStoredDigestForIdenticalContent(t *testing.T) {
	ctx := context.Background()
	for _, version := range []string{"go1.26", "go1.27"} {
		for _, withPlaceholder := range []bool{true, false} {
			t.Run("stored-by-"+version+"-placeholder="+map[bool]string{true: "stored", false: "absent"}[withPlaceholder], func(t *testing.T) {
				stored := readReuseFixture(t, version)
				built := encodeReuseArchives(t, stored.artifact)
				rev := revisionBuiltFrom(t, built, store.RevisionDigest(stored.artifact.Manifest.ContentDigest))
				originalManifest := rev.ManifestDigest
				meta := reuseMeta{stored: store.ResourceRevision{Coordinate: rev.Resources[0].Coordinate, ContentDigest: rev.Resources[0].ContentDigest, ArchiveDigest: stored.digest}}
				blobs := reuseBlobs{data: map[store.ArchiveDigest][]byte{stored.digest: stored.final}}
				if withPlaceholder {
					blobs.data[store.PlaceholderArchiveKey(stored.digest)] = stored.placeholder
				}

				if err := ReuseStoredArchives(ctx, meta, blobs, rev); err != nil {
					t.Fatalf("ReuseStoredArchives: %v", err)
				}
				if rev.Resources[0].ArchiveDigest != stored.digest {
					t.Fatalf("archive digest = %s, want stored %s", rev.Resources[0].ArchiveDigest, stored.digest)
				}
				if stored.digest != built.digest && rev.ManifestDigest == originalManifest {
					t.Fatal("registry revision manifest digest was not rebuilt for the reused archive")
				}
				if rev.ManifestDigest != rev.Occurrence.RegistryRevisionManifestDigest || rev.ResourceOccurrences[0].ManifestDigest != rev.ManifestDigest {
					t.Fatal("occurrence manifest digests disagree with the rebuilt registry revision")
				}
				manifest, _, err := artifact.DecodeRegistryRevisionManifest(bytes.NewReader(rev.ManifestPayload))
				if err != nil {
					t.Fatal(err)
				}
				if manifest.Resources[0].ArchiveDigest != string(stored.digest) {
					t.Fatalf("manifest archive digest = %s, want %s", manifest.Resources[0].ArchiveDigest, stored.digest)
				}
				if got, ok := blobData(rev.ExtraBlobs, stored.digest); !ok || !bytes.Equal(got, stored.final) {
					t.Fatal("stored final archive was not uploaded under its reused digest")
				}
				// An identical digest is not a substitution, so the built placeholder stays.
				wantPlaceholder := withPlaceholder || stored.digest == built.digest
				_, hasPlaceholder := blobData(rev.ExtraBlobs, store.PlaceholderArchiveKey(stored.digest))
				if hasPlaceholder != wantPlaceholder {
					t.Fatalf("placeholder upload present = %v, want %v", hasPlaceholder, wantPlaceholder)
				}
				if wantPlaceholder {
					if got, _ := blobData(rev.ExtraBlobs, store.PlaceholderArchiveKey(stored.digest)); !bytes.Equal(got, stored.placeholder) {
						t.Fatal("stored placeholder archive was not uploaded under its reused key")
					}
				}
				if stored.digest != built.digest {
					if _, ok := blobData(rev.ExtraBlobs, built.digest); ok {
						t.Fatal("newly built archive still uploaded under a digest the revision no longer carries")
					}
					if _, ok := blobData(rev.ExtraBlobs, store.PlaceholderArchiveKey(built.digest)); ok {
						t.Fatal("newly built placeholder archive still uploaded under a digest the revision no longer carries")
					}
				}
			})
		}
	}
}

// spec: marketplace-publication/drifted-content-still-publishes-a-new-archive
func TestReuseStoredArchivesLeavesDriftedContentAlone(t *testing.T) {
	ctx := context.Background()
	built := encodeReuseArchives(t, readReuseFixture(t, "go1.26").artifact)
	driftedArtifact := readReuseFixture(t, "go1.26").artifact
	driftedArtifact.Assets = map[string][]byte{"SUPPORT.md": []byte("drifted content")}
	stored := encodeReuseArchives(t, driftedArtifact)
	rev := revisionBuiltFrom(t, built, store.RevisionDigest(built.artifact.Manifest.ContentDigest))
	meta := reuseMeta{stored: store.ResourceRevision{Coordinate: rev.Resources[0].Coordinate, ContentDigest: rev.Resources[0].ContentDigest, ArchiveDigest: stored.digest}}
	blobs := reuseBlobs{data: map[store.ArchiveDigest][]byte{
		stored.digest: stored.final,
		store.PlaceholderArchiveKey(stored.digest): stored.placeholder,
	}}

	if err := ReuseStoredArchives(ctx, meta, blobs, rev); err != nil {
		t.Fatalf("ReuseStoredArchives: %v", err)
	}
	if rev.Resources[0].ArchiveDigest != built.digest {
		t.Fatalf("drifted content reused stored digest %s", rev.Resources[0].ArchiveDigest)
	}
	if got, ok := blobData(rev.ExtraBlobs, built.digest); !ok || !bytes.Equal(got, built.final) {
		t.Fatal("drifted content did not keep its newly built archive")
	}
	if strings.Contains(string(rev.ManifestPayload), string(stored.digest)) {
		t.Fatal("registry revision manifest carries the drifted stored digest")
	}
}
