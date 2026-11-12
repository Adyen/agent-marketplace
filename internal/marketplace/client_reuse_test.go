package marketplace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestResolverEndpointClientReuse(t *testing.T) {
	type resourceFixture struct {
		pin           config.MarketplaceResourcePin
		revision      store.AssessedResourceRevision
		raw           []byte
		archiveDigest string
	}
	fixtures := make(map[string]resourceFixture)
	pins := make([]config.MarketplaceResourcePin, 0, 2)
	for _, name := range []string{"review", "explain"} {
		artifact, err := hccartifact.FromIR(ir.NewSkill(name, name, "Marketplace reuse fixture.", []string{"user"}, nil, ir.SourceRef{}))
		if err != nil {
			t.Fatalf("FromIR(%q): %v", name, err)
		}
		digest := artifact.Manifest.ContentDigest
		artifact.Manifest.RegistryID = "src"
		artifact.Manifest.RegistrySlug = "source-slug"
		artifact.Manifest.Revision = "refs/heads/main"
		raw, archiveDigest, err := hccartifact.Encode(artifact)
		if err != nil {
			t.Fatalf("Encode(%q): %v", name, err)
		}
		pin := config.MarketplaceResourcePin{
			RegistryID: "src", RegistrySlug: "source-slug", Kind: "skill", Name: name,
			Revision: "refs/heads/main", Digest: digest,
		}
		revision := assessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: name},
			ContentDigest: store.RevisionDigest(digest),
			ArchiveDigest: store.ArchiveDigest(archiveDigest),
		})
		fixtures[name] = resourceFixture{pin: pin, revision: revision, raw: raw, archiveDigest: archiveDigest}
		pins = append(pins, pin)
	}

	var wellKnownRequests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) {
		wellKnownRequests.Add(1)
		writeTestWellKnown(w, "mp")
	})
	mux.HandleFunc("/api/v1/registries/source-slug", func(w http.ResponseWriter, _ *http.Request) {
		writeTestRegistry(w, store.Registry{
			ID: "src", Slug: "source-slug", Origins: []string{"https://git.example/repo.git"},
			Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}},
			Acquisition: store.AcquisitionMirror, Listed: true,
		})
	})
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/resources/"), "/")
		var fixture resourceFixture
		switch {
		case len(parts) == 5 && parts[0] == "src" && parts[1] == "skill" && parts[3] == "revisions":
			fixture = fixtures[parts[2]]
			if fixture.pin.Name == "" || parts[4] != fixture.pin.Digest {
				http.NotFound(w, r)
				return
			}
		case len(parts) == 3 && parts[0] == "source-slug" && parts[1] == "skill":
			fixture = fixtures[parts[2]]
			if fixture.pin.Name == "" {
				http.NotFound(w, r)
				return
			}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(fixture.revision)
	})
	mux.HandleFunc("/api/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		for _, fixture := range fixtures {
			want := "/api/v1/artifacts/" + strings.Replace(fixture.archiveDigest, ":", "/", 1)
			if r.URL.Path == want {
				_, _ = w.Write(fixture.raw)
				return
			}
		}
		http.NotFound(w, r)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx := context.Background()
	entry := config.MarketplaceEntry{URL: server.URL, InstanceID: "mp", Resources: pins}
	r := &Resolver{Cache: NewCache(t.TempDir())}
	resolved, errs := r.ResolveWithPolicy(ctx, &config.Config{Marketplaces: []config.MarketplaceEntry{entry}}, nil, resolver.MarketplaceResolveModeRefresh, nil)
	if len(errs) != 0 || len(resolved) != len(pins) {
		t.Fatalf("ResolveWithPolicy artifacts=%d errors=%v", len(resolved), errs)
	}
	if _, err := r.PrepareLocator(ctx, entry, MarketplaceLocator{Registry: "source-slug", Kind: "skill", Name: "review"}, nil); err != nil {
		t.Fatalf("PrepareLocator: %v", err)
	}
	if _, _, err := r.LatestRevision(ctx, entry, pins[0]); err != nil {
		t.Fatalf("LatestRevision: %v", err)
	}
	if got := wellKnownRequests.Load(); got != 1 {
		t.Fatalf("well-known requests across one Resolver lifetime = %d, want 1", got)
	}
}

func TestClientRegistryReuseKeysByRequestedRoute(t *testing.T) {
	var registryRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/registries/a" && r.URL.Path != "/api/v1/registries/X" {
			http.NotFound(w, r)
			return
		}
		registryRequests.Add(1)
		registryID, slug := "X", "a"
		if r.URL.Path == "/api/v1/registries/X" {
			registryID, slug = "b", "X"
		}
		writeTestRegistry(w, store.Registry{
			ID: store.RegistryID(registryID), Slug: slug, Origins: []string{"https://git.example/repo.git"},
			Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}},
			Acquisition: store.AcquisitionMirror, Listed: true,
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	for range 2 {
		if _, err := client.GetRegistryBySlug(context.Background(), "a"); err != nil {
			t.Fatalf("GetRegistryBySlug: %v", err)
		}
	}
	registry, err := client.GetRegistryBySlug(context.Background(), "X")
	if err != nil {
		t.Fatalf("GetRegistryBySlug(X): %v", err)
	}
	if registry.ID != "b" {
		t.Fatalf("registry ID = %q, want b", registry.ID)
	}
	if got := registryRequests.Load(); got != 2 {
		t.Fatalf("registry requests = %d, want 2", got)
	}
}

func TestResolverIdentityReuseIsPerLifetime(t *testing.T) {
	var wellKnownRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-marketplace.json" {
			http.NotFound(w, r)
			return
		}
		wellKnownRequests.Add(1)
		writeTestWellKnown(w, "mp")
	}))
	defer server.Close()

	cfg := &config.Config{Marketplaces: []config.MarketplaceEntry{{URL: server.URL, InstanceID: "mp"}}}
	for range 2 {
		if _, errs := (&Resolver{}).ResolveWithPolicy(context.Background(), cfg, nil, resolver.MarketplaceResolveModeRefresh, nil); len(errs) != 0 {
			t.Fatalf("ResolveWithPolicy: %v", errs)
		}
	}
	if got := wellKnownRequests.Load(); got != 2 {
		t.Fatalf("well-known requests across two Resolver lifetimes = %d, want 2", got)
	}
}
