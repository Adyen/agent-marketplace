package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	marketplaceartifact "github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestPlaceRegistryManifestRequiresOptIn(t *testing.T) {
	server, meta, _, handler, _ := managedRouteFixture(t)
	server.cfg.Management.Mode = "file"

	base := marketplaceartifact.RevisionResource{
		Kind:          meta.rev.Coordinate.Kind,
		Name:          meta.rev.Coordinate.Name,
		ContentDigest: string(meta.rev.ContentDigest),
	}
	scopedRevision := boundPlaceRevision(t, meta.rev)
	scoped := marketplaceartifact.RevisionResource{
		Kind:          scopedRevision.Coordinate.Kind,
		Name:          scopedRevision.Coordinate.Name,
		ContentDigest: string(scopedRevision.ContentDigest),
	}
	virtual := marketplaceartifact.RevisionResource{
		Kind:          nameutil.TargetProfileKind,
		Name:          "invented-app",
		ContentDigest: "sha256:" + strings.Repeat("c", 64),
	}

	tests := []struct {
		name          string
		additional    *marketplaceartifact.RevisionResource
		requiresPlace bool
	}{
		{name: "legacy only"},
		{name: "scoped package", additional: &scoped, requiresPlace: true},
		{name: "virtual target profile", additional: &virtual, requiresPlace: true},
	}
	requests := []struct {
		name     string
		features []string
		optedIn  bool
	}{
		{name: "legacy"},
		{name: "opted in", features: []string{TargetProfilePlaceFeature}, optedIn: true},
		{name: "repeated feature", features: []string{TargetProfilePlaceFeature, TargetProfilePlaceFeature}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resources := []marketplaceartifact.RevisionResource{base}
			if test.additional != nil {
				resources = append(resources, *test.additional)
			}
			payload, digest, err := marketplaceartifact.CanonicalRegistryRevisionPayload(marketplaceartifact.RegistryRevisionManifest{
				SchemaVersion: marketplaceartifact.RegistryRevisionSchemaV1,
				Generator:     marketplaceartifact.RegistryRevisionGeneratorV1,
				Resources:     resources,
			})
			if err != nil {
				t.Fatalf("build registry manifest: %v", err)
			}
			if err := server.blobs.Put(context.Background(), store.ArchiveDigest(digest), payload); err != nil {
				t.Fatalf("put registry manifest: %v", err)
			}

			for _, requestCase := range requests {
				t.Run(requestCase.name, func(t *testing.T) {
					request := httptest.NewRequest(http.MethodGet, "/api/v1/manifests/"+digest, nil)
					for _, feature := range requestCase.features {
						request.Header.Add(placeFeaturesHeader, feature)
					}
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, request)

					wantStatus := http.StatusOK
					if test.requiresPlace && !requestCase.optedIn {
						wantStatus = http.StatusNotFound
					}
					if response.Code != wantStatus {
						t.Fatalf("manifest status = %d, want %d: %s", response.Code, wantStatus, response.Body.String())
					}
					if test.requiresPlace && !strings.Contains(strings.Join(response.Header().Values("Vary"), ","), placeFeaturesHeader) {
						t.Fatalf("Vary = %q, want %s", response.Header().Values("Vary"), placeFeaturesHeader)
					}
					if wantStatus != http.StatusOK {
						return
					}
					if response.Body.String() != string(payload) {
						t.Fatalf("manifest body = %q, want exact bytes %q", response.Body.String(), payload)
					}
					if got := response.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
						t.Fatalf("Cache-Control = %q, want immutable cache policy", got)
					}
				})
			}
		})
	}
}
