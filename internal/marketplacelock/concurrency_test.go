package marketplacelock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestResolveUsesBoundedConcurrencyAndPreservesRequestErrorOrder(t *testing.T) {
	names := []string{"pin-0", "pin-1", "pin-2"}
	var current, peak atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
	})
	mux.HandleFunc("/api/v1/registries/tools", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.RegistryDTO{ID: "src", Slug: "tools", Origins: []string{"https://git.example/repo.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
	})
	mux.HandleFunc("/api/v1/resources/tools/skill/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/v1/resources/tools/skill/")
		now := current.Add(1)
		for observed := peak.Load(); now > observed; observed = peak.Load() {
			if peak.CompareAndSwap(observed, now) {
				break
			}
		}
		time.Sleep(map[string]time.Duration{"pin-0": 180, "pin-1": 120, "pin-2": 60}[name] * time.Millisecond)
		current.Add(-1)
		http.NotFound(w, r)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	requests := make([]string, len(names))
	for i, name := range names {
		requests[i] = "tools/skill/" + name
	}
	layer := repoLayerWithRequests(t, server.URL, requests)
	resolverMP := &marketplace.Resolver{HTTPClient: server.Client(), Cache: marketplace.NewCache(t.TempDir())}
	_, err := Resolve(context.Background(), resolverMP, layer, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "resolve tools/skill/pin-0") {
		t.Fatalf("Resolve error = %v, want the first configured request's error", err)
	}
	if peak.Load() <= 1 || peak.Load() > 8 {
		t.Fatalf("peak in-flight requests = %d, want 1 < peak <= 8", peak.Load())
	}
}

func repoLayerWithRequests(t *testing.T, marketplaceURL string, requests []string) config.LayerIntent {
	t.Helper()
	repo, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(repo, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var lines strings.Builder
	for _, request := range requests {
		fmt.Fprintf(&lines, "    - %s\n", request)
	}
	data := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  public: {marketplace: %s}\nresources:\n  public:\n%s", marketplaceURL, lines.String())
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	layer := config.ReadIntent(context.Background(), config.LoadEffectiveOptions{RepoRoot: repo}).Layers[config.RepoCommitted]
	if err := layer.Err(); err != nil {
		t.Fatal(err)
	}
	return layer
}

func collectionRevisionFixture(name, digest string) store.AssessedResourceRevision {
	coordinate := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "collection", Name: name}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: coordinate, ContentDigest: store.RevisionDigest(digest), AnalyzerVersion: "test-analyzer",
		PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)), Decision: store.AssessmentDecisionAllow,
		InspectionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("d", 64)), AssessedAt: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		panic(err)
	}
	return store.AssessedResourceRevision{
		ResourceRevision: store.ResourceRevision{Coordinate: coordinate, ContentDigest: store.RevisionDigest(digest)},
		Assessment:       assessment,
		Occurrence:       store.ResourceOccurrence{Coordinate: coordinate, ContentDigest: store.RevisionDigest(digest), Commit: strings.Repeat("a", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64))},
	}
}

// spec: marketplace-delivery-security/collection-preparation-stays-within-the-lock-bootstrap-limit
func TestResolveSharesLockBootstrapWorkLimitAcrossSourceRequests(t *testing.T) {
	// One bundle costs 6,481 traversal units: 80 middle collections that each
	// list the same 80 leaves. Three bundles exceed the 13,981 limit together
	// although each fits alone.
	const width = 80
	payloads, digests := map[string][]byte{}, map[string]string{}
	add := func(name string, members, leaves []hccartifact.CollectionMemberEntry) hccartifact.CollectionMemberEntry {
		payload, digest, err := hccartifact.EncodeCollectionManifest(hccartifact.CollectionManifest{SchemaVersion: hccartifact.CollectionSchemaV1, SupportedSchemas: []string{hccartifact.CollectionSchemaV1}, Name: name, Members: members, Leaves: leaves})
		if err != nil {
			t.Fatal(err)
		}
		payloads[digest], digests[name] = payload, digest
		return hccartifact.CollectionMemberEntry{Kind: "collection", Name: name, RegistryID: "src", Revision: digest}
	}
	skill := hccartifact.CollectionMemberEntry{Kind: "skill", Name: "review", RegistryID: "src", Revision: "sha256:" + strings.Repeat("a", 64)}
	flattened := func(paths [][]string) []hccartifact.CollectionMemberEntry {
		leaf := skill
		leaf.InclusionPaths = paths
		return []hccartifact.CollectionMemberEntry{leaf}
	}
	leafName := func(j int) string { return fmt.Sprintf("leaf-%03d", j) }
	middleName := func(i int) string { return fmt.Sprintf("middle-%03d", i) }
	leaves := make([]hccartifact.CollectionMemberEntry, width)
	for j := range leaves {
		leaves[j] = add(leafName(j), []hccartifact.CollectionMemberEntry{skill}, nil)
	}
	middles := make([]hccartifact.CollectionMemberEntry, width)
	for i := range middles {
		var paths [][]string
		for j := range leaves {
			paths = append(paths, []string{middleName(i), leafName(j)})
		}
		middles[i] = add(middleName(i), leaves, flattened(paths))
	}
	requests := make([]string, 3)
	for b := range requests {
		name := fmt.Sprintf("bundle-%d", b)
		var paths [][]string
		for i := range middles {
			for j := range leaves {
				paths = append(paths, []string{name, middleName(i), leafName(j)})
			}
		}
		add(name, middles, flattened(paths))
		requests[b] = "tools/collection/" + name
	}
	var skillRequests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-marketplace.json", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp", "https://example.com"))
	})
	mux.HandleFunc("/api/v1/registries/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.RegistryDTO{ID: "src", Slug: "tools", Origins: []string{"https://git.example/repo.git"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
	})
	mux.HandleFunc("/api/v1/resources/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		if parts[5] == "skill" {
			skillRequests.Add(1)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(collectionRevisionFixture(parts[6], digests[parts[6]]))
	})
	mux.HandleFunc("/api/v1/manifests/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payloads[strings.TrimPrefix(r.URL.Path, "/api/v1/manifests/")])
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	layer := repoLayerWithRequests(t, server.URL, requests)
	resolverMP := &marketplace.Resolver{HTTPClient: server.Client(), Cache: marketplace.NewCache(t.TempDir())}
	_, err := Resolve(context.Background(), resolverMP, layer, nil, nil)
	if err == nil {
		t.Fatal("Resolve succeeded, want the shared lock-bootstrap work limit to reject the source")
	}
	// Leaf preparation starts only after a bundle has reserved its full
	// traversal (6,481 units), so at most two of the three bundles can reach it
	// within the shared 13,981-unit budget.
	if got := skillRequests.Load(); got > 2 {
		t.Fatalf("leaf skill requests = %d, want at most 2 bundles to reach leaf preparation", got)
	}
}
