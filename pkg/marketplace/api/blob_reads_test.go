package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

type payloadReadProbe struct {
	store.BlobStore
	data                 []byte
	dataByDigest         map[store.ArchiveDigest][]byte
	requested            []store.ArchiveDigest
	getErr               error
	bounded, unbounded   int
	readBytes, lastLimit int64
}

func (s *payloadReadProbe) dataFor(digest store.ArchiveDigest) []byte {
	s.requested = append(s.requested, digest)
	if data, ok := s.dataByDigest[digest]; ok {
		return data
	}
	return s.data
}

func (s *payloadReadProbe) Get(_ context.Context, digest store.ArchiveDigest) ([]byte, error) {
	s.unbounded++
	data := s.dataFor(digest)
	s.readBytes += int64(len(data))
	return bytes.Clone(data), s.getErr
}

func (s *payloadReadProbe) GetBounded(_ context.Context, digest store.ArchiveDigest, limit int64) ([]byte, error) {
	s.bounded++
	s.lastLimit = limit
	return store.ReadBlobBounded(&payloadCountingReader{Reader: bytes.NewReader(s.dataFor(digest)), probe: s}, limit)
}

type payloadCountingReader struct {
	io.Reader
	probe *payloadReadProbe
}

func (r *payloadCountingReader) Read(data []byte) (int, error) {
	n, err := r.Reader.Read(data)
	r.probe.readBytes += int64(n)
	return n, err
}

func payloadReadServer(tb testing.TB, blobs store.BlobStore) *Server {
	tb.Helper()
	meta := sqlite.New(":memory:")
	if err := meta.Open(context.Background()); err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = meta.Close() })
	cfg := config.Config{Instance: config.Instance{ID: "mp-test"}}
	cfg.Policy.Limits.FileBytes = 16 << 10
	cfg.Policy.Limits.ArtifactBytes = 32 << 10
	return NewServer(cfg, meta, blobs)
}

// spec: marketplace-api/oversized-payloads-stop-at-the-existing-read-ceiling
func TestPublicPayloadReadsStopAtExistingCeilings(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, fixture := range []struct {
		path   string
		limit  int64
		status int
	}{
		{"/api/v1/content/sha256/" + digest, 16 << 10, http.StatusRequestEntityTooLarge},
		{"/api/v1/inspection/sha256/" + digest, inspection.MaxManifestBytes, http.StatusRequestEntityTooLarge},
		{"/api/v1/manifests/sha256:" + digest, 32 << 10, http.StatusInternalServerError},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+fixture.path, func(t *testing.T) {
				probe := &payloadReadProbe{
					BlobStore: fsblob.New(t.TempDir()),
					data:      bytes.Repeat([]byte("x"), int(fixture.limit)+1024),
				}
				server := payloadReadServer(t, probe)
				response := httptest.NewRecorder()
				server.PublicHandler().ServeHTTP(response, httptest.NewRequest(method, fixture.path, nil))
				if response.Code != fixture.status {
					t.Fatalf("status = %d, want %d", response.Code, fixture.status)
				}
				if probe.unbounded != 0 || probe.bounded != 1 || probe.lastLimit != fixture.limit || probe.readBytes > fixture.limit+1 {
					t.Fatalf("unbounded=%d bounded=%d limit=%d read=%d", probe.unbounded, probe.bounded, probe.lastLimit, probe.readBytes)
				}
				if strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
					t.Fatal("size refusal has immutable success caching")
				}
			})
		}
	}
}

// spec: marketplace-api/healthy-payload-reads-retain-response-identity
func TestHealthyPayloadReadRetainsIdentity(t *testing.T) {
	data := []byte("ordinary inert content")
	probe := &payloadReadProbe{BlobStore: fsblob.New(t.TempDir()), data: data}
	server := payloadReadServer(t, probe)
	path := "/api/v1/content/" + strings.Replace(inspection.Digest(data), ":", "/", 1)
	response := httptest.NewRecorder()
	server.PublicHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), data) {
		t.Fatalf("healthy content status = %d, body = %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "text/plain" || !strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("healthy content headers = %v", response.Header())
	}
	if probe.bounded != 1 || probe.unbounded != 0 || probe.readBytes != int64(len(data)) {
		t.Fatalf("healthy read: bounded=%d unbounded=%d bytes=%d", probe.bounded, probe.unbounded, probe.readBytes)
	}
}

func TestPayloadReadsRetainLimitAndCustomStoreCompatibility(t *testing.T) {
	data := []byte("ordinary inert content")
	for _, fixture := range []struct {
		name    string
		limit   int64
		legacy  bool
		status  int
		bounded int
	}{
		{"exact", int64(len(data)), false, http.StatusOK, 1},
		{"oversized", int64(len(data) - 1), false, http.StatusRequestEntityTooLarge, 1},
		{"unlimited", 0, false, http.StatusOK, 0},
		{"negative", -1, false, http.StatusOK, 0},
		{"maximum", math.MaxInt64, false, http.StatusOK, 0},
		{"legacy", int64(len(data)), true, http.StatusOK, 0},
		{"legacy-oversized", int64(len(data) - 1), true, http.StatusRequestEntityTooLarge, 0},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			probe := &payloadReadProbe{BlobStore: fsblob.New(t.TempDir()), data: data}
			var blobs store.BlobStore = probe
			if fixture.legacy {
				blobs = struct{ store.BlobStore }{probe}
			}
			server := payloadReadServer(t, blobs)
			server.cfg.Policy.Limits.FileBytes = fixture.limit
			path := "/api/v1/content/" + strings.Replace(inspection.Digest(data), ":", "/", 1)
			response := httptest.NewRecorder()
			server.PublicHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != fixture.status || probe.bounded != fixture.bounded || probe.unbounded != 1-fixture.bounded {
				t.Fatalf("status=%d bounded=%d unbounded=%d", response.Code, probe.bounded, probe.unbounded)
			}
			if fixture.status == http.StatusOK && !bytes.Equal(response.Body.Bytes(), data) {
				t.Fatal("healthy content changed")
			}
		})
	}
}

func TestLegacyPayloadReadErrorsRetainServerErrorStatus(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, fixture := range []struct {
		name, path string
		limit      int64
		legacy     bool
	}{
		{"content", "/api/v1/content/sha256/" + digest, 16 << 10, true},
		{"inspection", "/api/v1/inspection/sha256/" + digest, 16 << 10, true},
		{"unlimited", "/api/v1/content/sha256/" + digest, 0, false},
		{"maximum", "/api/v1/content/sha256/" + digest, math.MaxInt64, false},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(fixture.name+"/"+method, func(t *testing.T) {
				probe := &payloadReadProbe{
					BlobStore: fsblob.New(t.TempDir()),
					getErr:    fmt.Errorf("legacy backend read: %w", store.ErrBlobTooLarge),
				}
				var blobs store.BlobStore = probe
				if fixture.legacy {
					blobs = struct{ store.BlobStore }{probe}
				}
				server := payloadReadServer(t, blobs)
				server.cfg.Policy.Limits.FileBytes = fixture.limit
				response := httptest.NewRecorder()
				server.PublicHandler().ServeHTTP(response, httptest.NewRequest(method, fixture.path, nil))
				if response.Code != http.StatusInternalServerError || probe.unbounded != 1 || probe.bounded != 0 {
					t.Fatalf("status=%d unbounded=%d bounded=%d", response.Code, probe.unbounded, probe.bounded)
				}
			})
		}
	}
}

func resourceInspectionReadServer(t *testing.T, body []byte) (*Server, inspection.Projection) {
	t.Helper()
	ctx := context.Background()
	blobs := fsblob.New(t.TempDir())
	server := payloadReadServer(t, blobs)
	source := store.Registry{ID: "source-id", Slug: "source", Ref: "main", Acquisition: store.AcquisitionIndexOnly}
	if err := server.meta.PutRegistry(ctx, source); err != nil {
		t.Fatal(err)
	}
	coordinate := store.ResourceCoordinate{Marketplace: "mp-test", Registry: source.ID, Kind: KindSkill, Name: "example"}
	revision := store.RevisionDigest("sha256:" + strings.Repeat("a", 64))
	projection, err := inspection.Build(coordinate, string(revision), body, nil, 10, int64(len(body)), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	for digest, data := range projection.Bodies {
		if err := blobs.Put(ctx, store.ArchiveDigest(digest), data); err != nil {
			t.Fatal(err)
		}
	}
	if err := blobs.Put(ctx, store.ArchiveDigest(projection.Manifest.ManifestDigest), projection.Payload); err != nil {
		t.Fatal(err)
	}
	publishAssessedTestRevision(ctx, t, server.meta,
		store.ResourceRevision{Coordinate: coordinate, ContentDigest: revision},
		store.AssessmentDecisionAllow, store.RevisionDigest(projection.Manifest.ManifestDigest), strings.Repeat("b", 40))
	return server, projection
}

func TestResourceInspectionRetainsOversizeManifestStatus(t *testing.T) {
	body := []byte("# Ordinary inert content\n")
	server, projection := resourceInspectionReadServer(t, body)
	blobs := server.blobs
	base := "/api/v1/resources/source/skill/example/revisions/" + projection.Manifest.RevisionDigest + "/inspection"
	for _, fixture := range []struct {
		suffix string
		body   []byte
	}{
		{"", projection.Payload},
		{"/content", body},
		{"/content/" + projection.Manifest.Files[0].ContentDigest, body},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+fixture.suffix, func(t *testing.T) {
				server.blobs = blobs
				response := httptest.NewRecorder()
				server.PublicHandler().ServeHTTP(response, httptest.NewRequest(method, base+fixture.suffix, nil))
				if response.Code != http.StatusOK || method == http.MethodGet && !bytes.Equal(response.Body.Bytes(), fixture.body) {
					t.Fatalf("healthy inspection status=%d body=%q", response.Code, response.Body.Bytes())
				}
				for _, legacy := range []bool{false, true} {
					// Every route must stop at manifest admission, before any content read.
					probe := &payloadReadProbe{
						BlobStore: blobs,
						data:      bytes.Repeat([]byte("x"), int(inspection.MaxManifestBytes)+1024),
					}
					server.blobs = probe
					if legacy {
						server.blobs = struct{ store.BlobStore }{probe}
					}
					response = httptest.NewRecorder()
					server.PublicHandler().ServeHTTP(response, httptest.NewRequest(method, base+fixture.suffix, nil))
					if !slices.Equal(probe.requested, []store.ArchiveDigest{store.ArchiveDigest(projection.Manifest.ManifestDigest)}) {
						t.Fatalf("manifest refusal requested unexpected content: %v", probe.requested)
					}
					if response.Code != http.StatusInternalServerError || strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
						t.Fatalf("legacy=%v status=%d headers=%v", legacy, response.Code, response.Header())
					}
					if legacy {
						if probe.unbounded != 1 || probe.bounded != 0 || probe.readBytes != int64(len(probe.data)) {
							t.Fatalf("legacy read: unbounded=%d bounded=%d bytes=%d", probe.unbounded, probe.bounded, probe.readBytes)
						}
					} else if probe.unbounded != 0 || probe.bounded != 1 || probe.lastLimit != inspection.MaxManifestBytes || probe.readBytes != inspection.MaxManifestBytes+1 {
						t.Fatalf("bounded read: unbounded=%d bounded=%d limit=%d bytes=%d", probe.unbounded, probe.bounded, probe.lastLimit, probe.readBytes)
					}
				}
			})
		}
	}
}

// spec: marketplace-api/oversized-payloads-stop-at-the-existing-read-ceiling
func TestResourceInspectionContentReadsStopAtExistingCeiling(t *testing.T) {
	body := bytes.Repeat([]byte("x"), (16<<10)+1024)
	server, projection := resourceInspectionReadServer(t, body)
	blobs := server.blobs
	manifestDigest := store.ArchiveDigest(projection.Manifest.ManifestDigest)
	contentDigest := store.ArchiveDigest(projection.Manifest.Files[0].ContentDigest)
	base := "/api/v1/resources/source/skill/example/revisions/" + projection.Manifest.RevisionDigest + "/inspection/content"

	for _, suffix := range []string{"", "/" + string(contentDigest)} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, backend := range []string{"bounded", "legacy"} {
				for _, fixture := range []struct {
					name   string
					limit  int64
					status int
				}{
					{"exact", int64(len(body)), http.StatusOK},
					{"oversized", 16 << 10, http.StatusRequestEntityTooLarge},
				} {
					t.Run(method+suffix+"/"+backend+"/"+fixture.name, func(t *testing.T) {
						probe := &payloadReadProbe{
							BlobStore: blobs,
							dataByDigest: map[store.ArchiveDigest][]byte{
								manifestDigest: projection.Payload,
								contentDigest:  body,
							},
						}
						server.blobs = probe
						if backend == "legacy" {
							server.blobs = struct{ store.BlobStore }{probe}
						}
						server.cfg.Policy.Limits.FileBytes = fixture.limit
						response := httptest.NewRecorder()
						server.PublicHandler().ServeHTTP(response, httptest.NewRequest(method, base+suffix, nil))
						if !slices.Equal(probe.requested, []store.ArchiveDigest{manifestDigest, contentDigest}) {
							t.Fatalf("requested = %v, want manifest then content", probe.requested)
						}
						if response.Code != fixture.status {
							t.Fatalf("status = %d, want %d", response.Code, fixture.status)
						}
						wantBytes := int64(len(projection.Payload) + len(body))
						if backend == "legacy" {
							if probe.unbounded != 2 || probe.bounded != 0 {
								t.Fatalf("legacy reads: unbounded=%d bounded=%d", probe.unbounded, probe.bounded)
							}
						} else {
							if probe.unbounded != 0 || probe.bounded != 2 || probe.lastLimit != fixture.limit {
								t.Fatalf("bounded reads: unbounded=%d bounded=%d limit=%d", probe.unbounded, probe.bounded, probe.lastLimit)
							}
							if fixture.status == http.StatusRequestEntityTooLarge {
								wantBytes = int64(len(projection.Payload)) + fixture.limit + 1
							}
						}
						if probe.readBytes != wantBytes {
							t.Fatalf("read bytes = %d, want %d", probe.readBytes, wantBytes)
						}
						if fixture.status == http.StatusOK {
							if method == http.MethodGet && !bytes.Equal(response.Body.Bytes(), body) {
								t.Fatal("healthy content changed")
							}
							if response.Header().Get("Content-Type") != inspection.MediaMarkdown || !strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
								t.Fatalf("healthy content headers = %v", response.Header())
							}
						} else if strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
							t.Fatal("oversized content refusal has immutable success caching")
						}
					})
				}
			}
		}
	}
}
