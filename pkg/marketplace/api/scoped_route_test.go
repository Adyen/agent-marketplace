package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type scopedRouteMetadata struct {
	store.MetadataStore
	registryReads int
	lookups       []scopedRouteLookup
}

type scopedRouteLookup struct {
	method     string
	coordinate store.ResourceCoordinate
}

func (m *scopedRouteMetadata) GetRegistryBySlug(_ context.Context, slug string) (store.Registry, error) {
	m.registryReads++
	return store.Registry{ID: "registry", Slug: slug}, nil
}

func (m *scopedRouteMetadata) GetLatestResourceRevision(_ context.Context, coord store.ResourceCoordinate) (store.AssessedResourceRevision, error) {
	m.lookups = append(m.lookups, scopedRouteLookup{method: "latest", coordinate: coord})
	return store.AssessedResourceRevision{}, store.ErrNotFound
}

func (m *scopedRouteMetadata) ListResourceRevisions(_ context.Context, coord store.ResourceCoordinate) ([]store.AssessedResourceRevision, error) {
	m.lookups = append(m.lookups, scopedRouteLookup{method: "list", coordinate: coord})
	return nil, store.ErrNotFound
}

func (m *scopedRouteMetadata) GetResourceRevision(_ context.Context, coord store.ResourceCoordinate, _ store.RevisionDigest) (store.AssessedResourceRevision, error) {
	m.lookups = append(m.lookups, scopedRouteLookup{method: "revision", coordinate: coord})
	return store.AssessedResourceRevision{}, store.ErrNotFound
}

func TestScopedPackageRoute(t *testing.T) {
	digest := "sha256:" + strings.Repeat("1", 64)
	tests := []struct {
		name       string
		path       string
		features   []string
		wantKind   string
		wantName   string
		wantMethod string
	}{
		{
			name:       "scoped detail",
			path:       "/api/v1/resources/source/team%2Fmod/example",
			features:   []string{"target-profile-place-v1"},
			wantKind:   "team/mod",
			wantName:   "example",
			wantMethod: "latest",
		},
		{
			name:       "scoped revisions",
			path:       "/api/v1/resources/source/team%2Fmod/example/revisions",
			features:   []string{"target-profile-place-v1"},
			wantKind:   "team/mod",
			wantName:   "example",
			wantMethod: "list",
		},
		{
			name:       "scoped revision",
			path:       "/api/v1/resources/source/team%2Fmod/example/revisions/" + digest,
			features:   []string{"target-profile-place-v1"},
			wantKind:   "team/mod",
			wantName:   "example",
			wantMethod: "revision",
		},
		{
			name:       "scoped inspection",
			path:       "/api/v1/resources/source/team%2Fmod/example/revisions/" + digest + "/inspection",
			features:   []string{"target-profile-place-v1"},
			wantKind:   "team/mod",
			wantName:   "example",
			wantMethod: "revision",
		},
		{
			name:       "virtual target profile",
			path:       "/api/v1/resources/source/target-profile/team",
			features:   []string{"target-profile-place-v1"},
			wantKind:   "target-profile",
			wantName:   "team",
			wantMethod: "latest",
		},
		{
			name:       "ordinary kind without feature",
			path:       "/api/v1/resources/source/skill/example",
			wantKind:   "skill",
			wantName:   "example",
			wantMethod: "latest",
		},
		{name: "scoped kind without feature", path: "/api/v1/resources/source/team%2Fmod/example"},
		{name: "scoped kind with another feature", path: "/api/v1/resources/source/team%2Fmod/example", features: []string{"target-profile-place-v1, other"}},
		{name: "scoped kind with duplicate feature values", path: "/api/v1/resources/source/team%2Fmod/example", features: []string{"target-profile-place-v1", "target-profile-place-v1"}},
		{name: "raw slash", path: "/api/v1/resources/source/team/mod/example", features: []string{"target-profile-place-v1"}},
		{name: "lowercase escape", path: "/api/v1/resources/source/team%2fmod/example", features: []string{"target-profile-place-v1"}},
		{name: "double escape", path: "/api/v1/resources/source/team%252Fmod/example", features: []string{"target-profile-place-v1"}},
		{name: "multiple kind escapes", path: "/api/v1/resources/source/team%2Fmod%2Fother/example", features: []string{"target-profile-place-v1"}},
		{name: "missing scoped identifier", path: "/api/v1/resources/source/team%2F/example", features: []string{"target-profile-place-v1"}},
		{name: "invalid target identifier", path: "/api/v1/resources/source/Team%2Fmod/example", features: []string{"target-profile-place-v1"}},
		{name: "invalid package name", path: "/api/v1/resources/source/team%2Fmod/.", features: []string{"target-profile-place-v1"}},
		{name: "invalid package name casing", path: "/api/v1/resources/source/team%2Fmod/Bad", features: []string{"target-profile-place-v1"}},
		{name: "encoded registry separator", path: "/api/v1/resources/source%2Fother/team%2Fmod/example", features: []string{"target-profile-place-v1"}},
		{name: "encoded name separator", path: "/api/v1/resources/source/team%2Fmod/example%2Fother", features: []string{"target-profile-place-v1"}},
		{name: "invalid virtual profile name", path: "/api/v1/resources/source/target-profile/Bad", features: []string{"target-profile-place-v1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := &scopedRouteMetadata{}
			server := &Server{meta: meta, mpID: "market"}
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			for _, feature := range tt.features {
				request.Header.Add("X-Agent-Marketplace-Features", feature)
			}
			response := httptest.NewRecorder()

			server.handleResources(response, request)

			if tt.wantKind == "" {
				if response.Code < http.StatusBadRequest || meta.registryReads != 0 || len(meta.lookups) != 0 {
					t.Fatalf("response = %d, registry reads = %d, resource lookups = %#v; want refusal before reads",
						response.Code, meta.registryReads, meta.lookups)
				}
				return
			}
			if meta.registryReads != 1 || len(meta.lookups) != 1 {
				t.Fatalf("registry reads = %d, resource lookups = %#v; want one registry and resource lookup",
					meta.registryReads, meta.lookups)
			}
			got := meta.lookups[0]
			if got.method != tt.wantMethod || got.coordinate != (store.ResourceCoordinate{
				Marketplace: "market",
				Registry:    "registry",
				Kind:        tt.wantKind,
				Name:        tt.wantName,
			}) {
				t.Fatalf("resource lookup = %#v, want method %q and kind %q", got, tt.wantMethod, tt.wantKind)
			}
		})
	}
}
