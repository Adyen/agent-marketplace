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
	repo, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(repo, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var requests strings.Builder
	for _, name := range names {
		fmt.Fprintf(&requests, "    - tools/skill/%s\n", name)
	}
	data := fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  public: {marketplace: %s}\nresources:\n  public:\n%s", server.URL, requests.String())
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	layer := config.ReadIntent(context.Background(), config.LoadEffectiveOptions{RepoRoot: repo}).Layers[config.RepoCommitted]
	if err := layer.Err(); err != nil {
		t.Fatal(err)
	}
	resolverMP := &marketplace.Resolver{HTTPClient: server.Client(), Cache: marketplace.NewCache(t.TempDir())}
	_, err := Resolve(context.Background(), resolverMP, layer, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "resolve tools/skill/pin-0") {
		t.Fatalf("Resolve error = %v, want the first configured request's error", err)
	}
	if peak.Load() <= 1 || peak.Load() > 8 {
		t.Fatalf("peak in-flight requests = %d, want 1 < peak <= 8", peak.Load())
	}
}
