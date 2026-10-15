package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	marketplaceartifact "github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

// toolchainArchive is an mcpserver archive recorded by one Go toolchain.
type toolchainArchive struct {
	version     string
	final       []byte
	placeholder []byte
	digest      store.ArchiveDigest
	manifest    hccartifact.CanonicalManifest
}

func readToolchainArchive(t *testing.T, version string) toolchainArchive {
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
	return toolchainArchive{version: version, final: final, placeholder: placeholder, digest: store.ArchiveDigest(a.Manifest.ArchiveDigest), manifest: a.Manifest}
}

func runningToolchain() string {
	return strings.Join(strings.SplitN(runtime.Version(), ".", 3)[:2], ".")
}

// newPlaceholderTestServer returns a metadata store, blob store, and config with
// one mirrored registry in which archives are published.
func newPlaceholderTestServer(t *testing.T) (store.MetadataStore, store.BlobStore, config.Config) {
	t.Helper()
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	cfg := config.Config{Instance: config.Instance{ID: "mp-test"}}
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: store.MarketplaceID(cfg.Instance.ID), Name: "Test Marketplace"}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutRegistry(ctx, store.Registry{ID: "src-test", Slug: "test-source", Ref: "main", Acquisition: store.AcquisitionMirror, Listed: true}); err != nil {
		t.Fatal(err)
	}
	return meta, fsblob.New(t.TempDir()), cfg
}

// publishToolchainArchive publishes the archive's mcpserver revision, uploading extra with it.
// failingPutBlobStore rejects every write so a best-effort store cannot succeed.
type failingPutBlobStore struct {
	store.BlobStore
}

var errReadOnlyBlobStore = errors.New("blob store is read-only")

func (failingPutBlobStore) Put(context.Context, store.ArchiveDigest, []byte) error {
	return errReadOnlyBlobStore
}

func (failingPutBlobStore) PutImmutable(context.Context, store.ArchiveDigest, []byte) error {
	return errReadOnlyBlobStore
}

func publishToolchainArchive(t *testing.T, meta store.MetadataStore, blobs store.BlobStore, cfg config.Config, archive toolchainArchive, extra []store.BlobUpload) {
	t.Helper()
	ctx := context.Background()
	coord := store.ResourceCoordinate{Marketplace: store.MarketplaceID(cfg.Instance.ID), Registry: "src-test", Kind: "mcpserver", Name: archive.manifest.Name}
	revision := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest(archive.manifest.ContentDigest), ArchiveDigest: archive.digest, License: "MIT"}
	manifestData, manifestDigest, err := marketplaceartifact.CanonicalRegistryRevisionPayload(marketplaceartifact.RegistryRevisionManifest{
		SchemaVersion: marketplaceartifact.RegistryRevisionSchemaV1,
		Generator:     "test source revision",
		Resources: []marketplaceartifact.RevisionResource{{
			Kind: coord.Kind, Name: coord.Name, ContentDigest: string(revision.ContentDigest), ArchiveDigest: string(archive.digest),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest := store.RevisionDigest(manifestDigest)
	projection, err := inspection.Build(coord, archive.manifest.ContentDigest, []byte(`{"mcpServers":{"genai-docs":{"command":"npx"}}}`), nil, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coord, ContentDigest: revision.ContentDigest, AnalyzerVersion: "1.0.0",
		PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest(projection.Manifest.ManifestDigest), AssessedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	inspectionBlobs := []store.BlobUpload{{Digest: store.ArchiveDigest(projection.Manifest.ManifestDigest), Data: projection.Payload}}
	for digest, body := range projection.Bodies {
		inspectionBlobs = append(inspectionBlobs, store.BlobUpload{Digest: store.ArchiveDigest(digest), Data: body})
	}
	if err := store.PublishRegistryRevision(ctx, meta, blobs, store.RegistryRevisionPublication{
		Occurrence: store.RegistryOccurrence{
			RegistryID: "src-test", Ref: "main", Commit: strings.Repeat("a", 40),
			RegistryRevisionManifestDigest: manifest, Acquisition: store.AcquisitionMirror, MirroredArtifactDigest: archive.digest,
		},
		Resources:           []store.ResourceRevision{revision},
		ResourceOccurrences: []store.ResourceOccurrence{{Coordinate: coord, ContentDigest: revision.ContentDigest, Commit: strings.Repeat("a", 40), ManifestDigest: manifest}},
		Assessments:         []store.ResourceAssessment{assessment},
		ManifestPayload:     manifestData,
		ExtraBlobs:          extra,
		InspectionBlobs:     inspectionBlobs,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func getBytes(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

func archiveDigestOf(data []byte) store.ArchiveDigest {
	h := sha256.Sum256(data)
	return store.ArchiveDigest("sha256:" + hex.EncodeToString(h[:]))
}

// spec: marketplace-api/placeholder-archive-route-serves-archive-digest-bytes
// spec: marketplace-api/missing-placeholder-archive-is-derived-only-when-reproducible
// spec: marketplace-api/artifact-route-serves-stored-final-archives-without-re-compression
func TestPlaceholderArchiveRouteServesVerifiedBytes(t *testing.T) {
	for _, version := range []string{"go1.26", "go1.27"} {
		t.Run("recorded-by-"+version, func(t *testing.T) {
			archive := readToolchainArchive(t, version)
			native := version == runningToolchain()

			t.Run("stored placeholder serves archive-digest bytes", func(t *testing.T) {
				meta, blobs, cfg := newPlaceholderTestServer(t)
				publishToolchainArchive(t, meta, blobs, cfg, archive, []store.BlobUpload{
					{Digest: archive.digest, Data: archive.final},
					{Digest: store.PlaceholderArchiveKey(archive.digest), Data: archive.placeholder},
				})
				public := httptest.NewServer(NewServer(cfg, meta, blobs).PublicHandler())
				defer public.Close()

				status, body := getBytes(t, public.URL+"/api/v1/archives/"+strings.Replace(string(archive.digest), ":", "/", 1))
				if status != http.StatusOK || archiveDigestOf(body) != archive.digest {
					t.Fatalf("placeholder route status %d, digest match %v", status, archiveDigestOf(body) == archive.digest)
				}
				status, body = getBytes(t, public.URL+"/api/v1/artifacts/"+strings.Replace(string(archive.digest), ":", "/", 1))
				if status != http.StatusOK || !bytes.Equal(body, archive.final) {
					t.Fatalf("artifact route status %d, want the stored final archive", status)
				}
			})

			t.Run("missing placeholder is derived only when reproducible", func(t *testing.T) {
				meta, blobs, cfg := newPlaceholderTestServer(t)
				publishToolchainArchive(t, meta, blobs, cfg, archive, nil)
				if err := blobs.Put(context.Background(), archive.digest, archive.final); err != nil {
					t.Fatal(err)
				}
				public := httptest.NewServer(NewServer(cfg, meta, blobs).PublicHandler())
				defer public.Close()

				status, body := getBytes(t, public.URL+"/api/v1/archives/"+strings.Replace(string(archive.digest), ":", "/", 1))
				if !native {
					if status != http.StatusNotFound {
						t.Fatalf("foreign placeholder status %d, want 404", status)
					}
					return
				}
				if status != http.StatusOK || archiveDigestOf(body) != archive.digest || !bytes.Equal(body, archive.placeholder) {
					t.Fatalf("derived placeholder status %d, digest match %v", status, archiveDigestOf(body) == archive.digest)
				}
			})

			t.Run("derived placeholder still serves when storing it fails", func(t *testing.T) {
				meta, blobs, cfg := newPlaceholderTestServer(t)
				publishToolchainArchive(t, meta, blobs, cfg, archive, nil)
				if err := blobs.Put(context.Background(), archive.digest, archive.final); err != nil {
					t.Fatal(err)
				}
				public := httptest.NewServer(NewServer(cfg, meta, failingPutBlobStore{BlobStore: blobs}).PublicHandler())
				defer public.Close()

				status, body := getBytes(t, public.URL+"/api/v1/archives/"+strings.Replace(string(archive.digest), ":", "/", 1))
				if !native {
					if status != http.StatusNotFound {
						t.Fatalf("foreign placeholder status %d, want 404", status)
					}
					return
				}
				if status != http.StatusOK || archiveDigestOf(body) != archive.digest {
					t.Fatalf("placeholder route with failing store status %d, digest match %v", status, archiveDigestOf(body) == archive.digest)
				}
			})
		})
	}
}
