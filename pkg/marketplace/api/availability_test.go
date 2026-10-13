package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type availabilityMetadata struct {
	store.MetadataStore
	store.ManagedConfigurationStore
	change store.ManagedChange
	err    error
	reads  int
}

func (m *availabilityMetadata) CurrentManagedConfiguration(context.Context, store.MarketplaceID) (store.ManagedChange, error) {
	m.reads++
	return m.change, m.err
}

func availabilityFixture(t testing.TB) (*Server, *availabilityMetadata, admin.Snapshot, store.AssessedResourceRevision) {
	t.Helper()
	snapshot := admin.Snapshot{Version: 1, PolicyRevision: 3, LicensePolicy: admin.LicensePolicy{Allowlist: []string{"MIT"}, Undeclared: "reject"}, Registries: []admin.Registry{{ID: "reg", Generation: 2, Lifecycle: "active", Acquisition: store.AcquisitionMirror, License: "Apache-2.0"}}}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	meta := &availabilityMetadata{change: store.ManagedChange{Marketplace: "market", Revision: 4, Snapshot: encoded}}
	server := NewServer(config.Config{Instance: config.Instance{ID: "market"}, Management: config.Management{Mode: "database"}}, meta, nil)
	rev := store.AssessedResourceRevision{ResourceRevision: store.ResourceRevision{Coordinate: store.ResourceCoordinate{Marketplace: "market", Registry: "reg", Kind: "skill", Name: "example"}, License: "MIT", ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)), ArchiveDigest: store.ArchiveDigest("sha256:" + strings.Repeat("b", 64))}, Assessment: store.ResourceAssessment{Decision: store.AssessmentDecisionAllow}}
	return server, meta, snapshot, rev
}

func TestManagedAvailabilityMatrix(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*admin.Snapshot, *store.AssessedResourceRevision)
		want Availability
	}{
		{"allowed immutable license not registry default", func(*admin.Snapshot, *store.AssessedResourceRevision) {}, AvailabilityAvailable},
		{"pause preserves", func(s *admin.Snapshot, _ *store.AssessedResourceRevision) {
			s.Registries[0].Paused = true
			s.Registries[0].Lifecycle = "paused"
		}, AvailabilityAvailable},
		{"withdraw", func(s *admin.Snapshot, _ *store.AssessedResourceRevision) { s.Registries[0].Withdrawn = true }, AvailabilityRevoked},
		{"retire", func(s *admin.Snapshot, _ *store.AssessedResourceRevision) { s.Registries[0].Retired = true }, AvailabilityRevoked},
		{"stricter policy", func(s *admin.Snapshot, _ *store.AssessedResourceRevision) {
			s.LicensePolicy.Allowlist = []string{"Apache-2.0"}
		}, AvailabilityPolicyBlocked},
		{"relax does not override rejection", func(s *admin.Snapshot, r *store.AssessedResourceRevision) {
			s.LicensePolicy.Allowlist = nil
			r.Assessment.Decision = store.AssessmentDecisionReject
		}, AvailabilityPolicyBlocked},
		{"global exposure preserves rejection", func(s *admin.Snapshot, r *store.AssessedResourceRevision) {
			s.DistributionPolicy.RejectedResources = config.RejectedResourcesExpose
			r.Assessment.Decision = store.AssessmentDecisionReject
		}, AvailabilityOperatorExposed},
		{"registry block overrides global exposure", func(s *admin.Snapshot, r *store.AssessedResourceRevision) {
			s.DistributionPolicy.RejectedResources = config.RejectedResourcesExpose
			s.Registries[0].RejectedResources = config.RejectedResourcesBlock
			r.Assessment.Decision = store.AssessmentDecisionReject
		}, AvailabilityPolicyBlocked},
		{"registry exposure overrides global block", func(s *admin.Snapshot, r *store.AssessedResourceRevision) {
			s.Registries[0].RejectedResources = config.RejectedResourcesExpose
			r.Assessment.Decision = store.AssessmentDecisionReject
		}, AvailabilityOperatorExposed},
		{"revocation precedes exposure", func(s *admin.Snapshot, r *store.AssessedResourceRevision) {
			s.DistributionPolicy.RejectedResources = config.RejectedResourcesExpose
			r.Assessment.Decision = store.AssessmentDecisionReject
			s.Registries[0].Retired = true
		}, AvailabilityRevoked},
		{"relax does not invent artifact", func(s *admin.Snapshot, r *store.AssessedResourceRevision) {
			s.LicensePolicy.Allowlist = nil
			r.ArchiveDigest = ""
		}, AvailabilityNoArtifact},
		{"undeclared reject", func(_ *admin.Snapshot, r *store.AssessedResourceRevision) { r.License = "" }, AvailabilityPolicyBlocked},
		{"undeclared warn", func(s *admin.Snapshot, r *store.AssessedResourceRevision) {
			s.LicensePolicy.Undeclared = "warn"
			r.License = " "
		}, AvailabilityAvailable},
		{"undeclared allow", func(s *admin.Snapshot, r *store.AssessedResourceRevision) {
			s.LicensePolicy.Undeclared = "allow"
			r.License = ""
		}, AvailabilityAvailable},
		{"empty allowlist disables enforcement", func(s *admin.Snapshot, r *store.AssessedResourceRevision) {
			s.LicensePolicy.Allowlist = nil
			r.License = ""
		}, AvailabilityAvailable},
		{"declared matching not trimmed", func(_ *admin.Snapshot, r *store.AssessedResourceRevision) { r.License = " MIT " }, AvailabilityPolicyBlocked},
		{"override", func(s *admin.Snapshot, r *store.AssessedResourceRevision) {
			s.Registries[0].LicenseOverride = &admin.LicensePolicy{Allowlist: []string{"Apache-2.0"}}
			r.License = "Apache-2.0"
		}, AvailabilityAvailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, meta, snapshot, rev := availabilityFixture(t)
			tc.edit(&snapshot, &rev)
			meta.change.Snapshot, _ = json.Marshal(snapshot)
			view, err := server.loadAvailability(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			got := server.evaluateAvailability(view, rev)
			if got.Availability != tc.want {
				t.Fatalf("decision=%+v want=%s", got, tc.want)
			}
			if got.ManagedRevision != 4 || got.PolicyRevision != 3 || got.RegistryGeneration != 2 {
				t.Fatalf("missing versions: %+v", got)
			}
			if rev.Assessment.Decision != got.AssessmentDecision {
				t.Fatal("assessment decision overwritten")
			}
		})
	}
}

// spec: marketplace-catalog-performance/unavailable-or-invalid-authority-rejects-public-requests
func TestManagedAvailabilityRejectsCorruptSnapshots(t *testing.T) {
	for _, mutate := range []func(*admin.Snapshot){
		func(s *admin.Snapshot) { s.Version = 0 },
		func(s *admin.Snapshot) { s.PolicyRevision = 0 },
		func(s *admin.Snapshot) { s.LicensePolicy.Undeclared = "typo" },
		func(s *admin.Snapshot) { s.DistributionPolicy.RejectedResources = "permit" },
		func(s *admin.Snapshot) { s.Registries[0].RejectedResources = "permit" },
		func(s *admin.Snapshot) { s.Registries[0].Generation = 0 },
		func(s *admin.Snapshot) { s.Registries[0].Lifecycle = "unknown" },
		func(s *admin.Snapshot) { s.Registries[0].Acquisition = "unknown" },
		func(s *admin.Snapshot) { s.Registries = append(s.Registries, s.Registries[0]) },
	} {
		server, meta, snapshot, _ := availabilityFixture(t)
		mutate(&snapshot)
		meta.change.Snapshot, _ = json.Marshal(snapshot)
		if _, err := server.loadAvailability(context.Background()); !errors.Is(err, store.ErrManagedStateCorrupt) {
			t.Fatalf("got %v", err)
		}
	}
}

func TestAvailabilityRequestSnapshotAndCache(t *testing.T) {
	server, meta, _, rev := availabilityFixture(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		view := availabilityFromContext(r.Context())
		if view == nil {
			t.Fatal("missing view")
		}
		first := server.evaluateAvailability(view, rev)
		meta.err = errors.New("database changed after request snapshot")
		second := server.evaluateAvailability(view, rev)
		if first != second {
			t.Fatal("incoherent request decisions")
		}
		writeImmutable(w, "text/plain", []byte("allowed"))
	})
	handler := server.availabilityHandler(next)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/content/sha256/test", nil))
	if response.Code != 200 || response.Body.String() != "allowed" || response.Header().Get("Cache-Control") != "private, no-cache, max-age=0, must-revalidate" {
		t.Fatalf("response=%d %s %v", response.Code, response.Body.String(), response.Header())
	}
	if meta.reads != 1 {
		t.Fatalf("reads=%d", meta.reads)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/content/sha256/test", nil))
	if response.Code != 503 {
		t.Fatalf("database failure=%d", response.Code)
	}
}
