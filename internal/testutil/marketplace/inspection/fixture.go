package inspectionfixture

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	marketplaceinspection "github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type Fixture struct {
	Server   *httptest.Server
	Revision string
	Manifest marketplaceinspection.Manifest
	Primary  []byte
	Support  map[string][]byte

	mu          sync.Mutex
	wellKnown   mpapi.WellKnown
	corruptNext map[string]bool
	paths       []string
}

type Mutate func(f *Fixture, wk *mpapi.WellKnown, rev *store.AssessedResourceRevision, manifestPayload *[]byte, bodies map[string][]byte)

func (f *Fixture) Log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

func (f *Fixture) SetWellKnownInstanceID(instanceID string) {
	f.mu.Lock()
	f.wellKnown.InstanceID = instanceID
	f.mu.Unlock()
}

func (f *Fixture) CorruptNextSupportBody(name string) bool {
	path := "support/" + name
	for _, file := range f.Manifest.Files {
		if file.Path == path {
			f.mu.Lock()
			f.corruptNext[file.ContentDigest] = true
			f.mu.Unlock()
			return true
		}
	}
	return false
}

func NewFixture(t *testing.T, mutate Mutate) *Fixture {
	return NewFixtureContent(t,
		[]byte("# Grill with docs\n\nPrimary inspection."),
		map[string][]byte{
			"notes.md": []byte("# Notes\n\nSupporting inspection."),
			"logo.png": []byte("\x89PNG\r\n\x1a\nfake-image"),
		}, mutate)
}

func NewFixtureContent(t *testing.T, primary []byte, support map[string][]byte, mutate Mutate) *Fixture {
	t.Helper()
	f := &Fixture{
		Revision:    "sha256:" + strings.Repeat("a", 64),
		Primary:     primary,
		Support:     support,
		corruptNext: map[string]bool{},
	}
	coord := store.ResourceCoordinate{Marketplace: "test-instance", Registry: "registry-1", Kind: "skill", Name: "grill"}
	projection, err := marketplaceinspection.Build(coord, f.Revision, f.Primary, f.Support, 10, 1<<20, 2<<20)
	if err != nil {
		t.Fatalf("inspection.Build: %v", err)
	}
	f.Manifest = projection.Manifest
	revision := store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest(f.Revision)}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate:               coord,
		ContentDigest:            revision.ContentDigest,
		AnalyzerVersion:          "tui-test",
		PolicyDigest:             store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
		Decision:                 store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest(projection.Manifest.ManifestDigest),
		AssessedAt:               time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("FinalizeResourceAssessment: %v", err)
	}
	rev := store.AssessedResourceRevision{
		ResourceRevision: revision,
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate:     coord,
			ContentDigest:  revision.ContentDigest,
			Commit:         strings.Repeat("a", 40),
			ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64)),
		},
	}
	wk := mpapi.DefaultWellKnown("test-instance", "https://market.example")
	manifestPayload := projection.Payload
	bodies := map[string][]byte{marketplaceinspection.Digest(f.Primary): f.Primary}
	for _, body := range f.Support {
		bodies[marketplaceinspection.Digest(body)] = body
	}
	if mutate != nil {
		mutate(f, &wk, &rev, &manifestPayload, bodies)
	}
	f.wellKnown = wk
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.mu.Unlock()
		switch {
		case r.URL.Path == "/.well-known/agent-marketplace.json":
			w.Header().Set("Content-Type", "application/json")
			f.mu.Lock()
			wellKnown := f.wellKnown
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(wellKnown)
		case r.URL.Path == "/api/v1/resources/registry-1/skill/grill/revisions/"+f.Revision:
			_ = json.NewEncoder(w).Encode(rev)
		case strings.HasSuffix(r.URL.Path, "/inspection"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(manifestPayload)
		case r.URL.Path == "/api/v1/resources/registry-1/skill/grill/revisions/"+f.Revision+"/inspection/content":
			w.Header().Set("Content-Type", marketplaceinspection.MediaMarkdown)
			_, _ = w.Write(f.Primary)
		case strings.HasPrefix(r.URL.Path, "/api/v1/resources/registry-1/skill/grill/revisions/"+f.Revision+"/inspection/content/"):
			digest := strings.TrimPrefix(r.URL.Path, "/api/v1/resources/registry-1/skill/grill/revisions/"+f.Revision+"/inspection/content/")
			body, ok := bodies[digest]
			if !ok {
				http.NotFound(w, r)
				return
			}
			media := marketplaceinspection.MediaMarkdown
			for _, file := range projection.Manifest.Files {
				if file.ContentDigest == digest {
					media = file.MediaType
				}
			}
			f.mu.Lock()
			corrupt := f.corruptNext[digest]
			delete(f.corruptNext, digest)
			f.mu.Unlock()
			if corrupt {
				body = append(append([]byte(nil), body...), '!')
			}
			w.Header().Set("Content-Type", media)
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Server.Close)
	return f
}

func (f *Fixture) Endpoint() config.MarketplaceEntry {
	return config.MarketplaceEntry{URL: f.Server.URL, InstanceID: "test-instance"}
}

func (f *Fixture) Item() mpapi.ResourceListItem {
	return mpapi.ResourceListItem{
		RegistryID: "registry-1", RegistrySlug: "tools", Kind: "skill", Name: "grill",
		Revision: f.Revision, ContentDigest: f.Revision,
		Availability: mpapi.AvailabilityAvailable, Description: "inspected grill",
	}
}
