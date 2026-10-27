package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// archiveReadProbe records how archive blobs are read: unbounded Get calls and
// the largest limit of any bounded read.
type archiveReadProbe struct {
	store.BlobStore
	unbounded map[store.ArchiveDigest]int
	limits    map[store.ArchiveDigest]int64
}

func newArchiveReadProbe(blobs store.BlobStore) *archiveReadProbe {
	return &archiveReadProbe{BlobStore: blobs, unbounded: map[store.ArchiveDigest]int{}, limits: map[store.ArchiveDigest]int64{}}
}

func (p *archiveReadProbe) Get(ctx context.Context, digest store.ArchiveDigest) ([]byte, error) {
	p.unbounded[digest]++
	return p.BlobStore.Get(ctx, digest)
}

func (p *archiveReadProbe) GetBounded(ctx context.Context, digest store.ArchiveDigest, limit int64) ([]byte, error) {
	p.limits[digest] = max(p.limits[digest], limit)
	return p.BlobStore.(store.BoundedBlobStore).GetBounded(ctx, digest, limit)
}

func (p *archiveReadProbe) requireBoundedArchiveReads(t *testing.T, digest store.ArchiveDigest, limit int64) {
	t.Helper()
	for _, key := range []store.ArchiveDigest{digest, store.PlaceholderArchiveKey(digest)} {
		if p.unbounded[key] != 0 {
			t.Errorf("archive blob %s was read without a bound %d times", key, p.unbounded[key])
		}
		if got := p.limits[key]; got > limit {
			t.Errorf("archive blob %s bounded at %d, above the artifact limit %d", key, got, limit)
		}
	}
}

// spec: marketplace-api/oversized-archives-stop-at-the-artifact-ceiling
func TestArchiveRoutesReadBlobsWithinTheArtifactLimit(t *testing.T) {
	archive := readToolchainArchive(t, runningToolchain())
	archivePath := strings.Replace(string(archive.digest), ":", "/", 1)
	for _, fixture := range []struct {
		name        string
		placeholder bool
		route       string
	}{
		{"artifact with stored placeholder", true, "/api/v1/artifacts/"},
		{"artifact without placeholder", false, "/api/v1/artifacts/"},
		{"stored placeholder", true, "/api/v1/archives/"},
		{"derived placeholder", false, "/api/v1/archives/"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, oversized := range []bool{false, true} {
				meta, blobs, cfg := newPlaceholderTestServer(t)
				extra := []store.BlobUpload{{Digest: archive.digest, Data: archive.final}}
				if fixture.placeholder {
					extra = append(extra, store.BlobUpload{Digest: store.PlaceholderArchiveKey(archive.digest), Data: archive.placeholder})
				}
				publishToolchainArchive(t, meta, blobs, cfg, archive, extra)
				limit := int64(len(archive.final) + len(archive.placeholder))
				if oversized {
					limit = int64(min(len(archive.final), len(archive.placeholder))) - 1
				}
				cfg.Policy.Limits.ArtifactBytes = limit
				probe := newArchiveReadProbe(blobs)
				response := httptest.NewRecorder()
				NewServer(cfg, meta, probe).PublicHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, fixture.route+archivePath, nil))
				want := http.StatusOK
				if oversized {
					want = http.StatusRequestEntityTooLarge
				}
				if response.Code != want {
					t.Fatalf("oversized=%t status = %d, want %d: %s", oversized, response.Code, want, response.Body.String())
				}
				if !oversized && fixture.route == "/api/v1/artifacts/" && !bytes.Equal(response.Body.Bytes(), archive.final) {
					t.Fatal("artifact route did not serve the stored final archive")
				}
				if !oversized && fixture.route == "/api/v1/archives/" && archiveDigestOf(response.Body.Bytes()) != archive.digest {
					t.Fatal("placeholder route did not serve bytes hashing to the archive digest")
				}
				if oversized && strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
					t.Fatal("size refusal has immutable success caching")
				}
				probe.requireBoundedArchiveReads(t, archive.digest, limit)
				if probe.limits[archive.digest] == 0 && probe.limits[store.PlaceholderArchiveKey(archive.digest)] == 0 {
					t.Fatal("no archive blob was read through the bounded interface")
				}
			}
		})
	}
}
