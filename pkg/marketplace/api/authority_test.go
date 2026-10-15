package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type authorityMetadata struct {
	store.MetadataStore
	store.ManagedConfigurationStore
	err error
}

func (m *authorityMetadata) CurrentManagedConfiguration(context.Context, store.MarketplaceID) (store.ManagedChange, error) {
	return store.ManagedChange{}, m.err
}

func (*assessmentRouteStore) CurrentManagedConfiguration(context.Context, store.MarketplaceID) (store.ManagedChange, error) {
	return store.ManagedChange{}, store.ErrNotFound
}
func (*catalogOnlyStore) CurrentManagedConfiguration(context.Context, store.MarketplaceID) (store.ManagedChange, error) {
	return store.ManagedChange{}, store.ErrNotFound
}
func (*mismatchedResourceStore) CurrentManagedConfiguration(context.Context, store.MarketplaceID) (store.ManagedChange, error) {
	return store.ManagedChange{}, store.ErrNotFound
}

// spec: marketplace-catalog-performance/unavailable-or-invalid-authority-rejects-public-requests
func TestPublicAuthorityFailsClosedAfterAdoption(t *testing.T) {
	meta := &authorityMetadata{err: store.ErrNotFound}
	server := NewServer(config.Config{Instance: config.Instance{ID: "market"}}, meta, nil)
	response := httptest.NewRecorder()
	server.PublicHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("legacy capabilities = %d", response.Code)
	}
	for _, state := range []struct {
		name string
		err  error
	}{
		{"adopted", nil}, {"corrupt", store.ErrManagedStateCorrupt}, {"outage", errors.New("database unavailable")},
	} {
		t.Run(state.name, func(t *testing.T) {
			meta.err = state.err
			want := state.err
			if want == nil {
				want = store.ErrAuthorityModeConflict
			}
			if err := server.Start(context.Background()); !errors.Is(err, want) {
				t.Fatalf("start = %v, want %v", err, want)
			}
			ready := httptest.NewRecorder()
			server.OpsHandler().ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if ready.Code != http.StatusServiceUnavailable {
				t.Fatalf("readiness = %d", ready.Code)
			}
			live := httptest.NewRecorder()
			server.PublicHandler().ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if live.Code != http.StatusOK {
				t.Fatalf("liveness = %d", live.Code)
			}
			for _, path := range []string{"/api/v1/capabilities", "/api/v1/search", "/api/v1/catalog", "/api/v1/registries", "/api/v1/registries/reg", "/api/v1/resources/reg/skill/name", "/api/v1/manifests/digest", "/api/v1/assessments/digest", "/api/v1/inspection/digest", "/api/v1/content/digest", "/api/v1/artifacts/digest", "/api/v1/placeholder-archives/digest"} {
				response := httptest.NewRecorder()
				server.PublicHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				if response.Code != http.StatusServiceUnavailable {
					t.Fatalf("%s = %d", path, response.Code)
				}
				if response.Header().Get("Cache-Control") != "private, no-store" {
					t.Fatalf("%s cache = %q", path, response.Header().Get("Cache-Control"))
				}
			}
		})
	}
}
