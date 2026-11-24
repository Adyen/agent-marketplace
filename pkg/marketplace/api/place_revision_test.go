package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestScopedExactRevisionRequiresOptInAndBinding(t *testing.T) {
	server, meta, _, _, _ := managedRouteFixture(t)
	server.cfg.Management.Mode = "file"
	coordinate := store.ResourceCoordinate{Marketplace: "market", Registry: "reg", Kind: "invented-app/mod", Name: "sample"}
	revision := meta.rev
	revision.Coordinate = coordinate
	revision.Occurrence.Coordinate = coordinate
	assessment := revision.Assessment
	assessment.Coordinate = coordinate
	assessment.ContentDigest = revision.ContentDigest
	profileDigest := "sha256:" + strings.Repeat("b", 64)
	binding := &targetprofile.Binding{
		PackageContentDigest:  string(revision.ContentDigest),
		DeliveryMappingDigest: "sha256:" + strings.Repeat("a", 64),
		ProfileDigest:         profileDigest,
		ProfileIdentity: targetprofile.Identity{
			TargetID: "invented-app", SourceID: "builtin:agent-marketplace", RevisionDigest: profileDigest,
		},
	}
	assessment.HostPackageIdentity = binding
	assessment.AssessmentDigest = ""
	var err error
	revision.Assessment, err = store.FinalizeResourceAssessment(assessment)
	if err != nil {
		t.Fatalf("FinalizeResourceAssessment: %v", err)
	}
	meta.rev = revision
	path := "/api/v1/resources/source/invented-app%2Fmod/sample/revisions/" + string(revision.ContentDigest)
	request := func(values ...string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for _, value := range values {
			req.Header.Add("X-Agent-Marketplace-Features", value)
		}
		return req
	}
	assertVary := func(response *httptest.ResponseRecorder) {
		t.Helper()
		if !strings.Contains(strings.Join(response.Header().Values("Vary"), ","), "X-Agent-Marketplace-Features") {
			t.Fatalf("Vary = %q, want X-Agent-Marketplace-Features", response.Header().Values("Vary"))
		}
	}

	response := httptest.NewRecorder()
	server.handleResources(response, request(TargetProfilePlaceFeature))
	if response.Code != http.StatusOK {
		t.Fatalf("opted-in exact revision status = %d: %s", response.Code, response.Body.String())
	}
	assertVary(response)
	var got store.AssessedResourceRevision
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode exact revision: %v", err)
	}
	if got.Coordinate != coordinate || got.ArchiveDigest != revision.ArchiveDigest ||
		got.Assessment.HostPackageIdentity == nil || *got.Assessment.HostPackageIdentity != *binding {
		t.Fatalf("exact revision = %+v, want coordinate, archive and binding", got)
	}
	if err := got.Assessment.Validate(); err != nil {
		t.Fatalf("response assessment validation: %v", err)
	}

	for _, headers := range [][]string{nil, {TargetProfilePlaceFeature, TargetProfilePlaceFeature}} {
		response = httptest.NewRecorder()
		server.handleResources(response, request(headers...))
		if response.Code == http.StatusOK || strings.Contains(response.Body.String(), "host_package_identity") ||
			strings.Contains(response.Body.String(), "builtin:agent-marketplace") {
			t.Fatalf("legacy/duplicate-feature response = %d/%s, want refusal without binding", response.Code, response.Body.String())
		}
		assertVary(response)
	}

	malformed := revision
	malformedAssessment := malformed.Assessment
	malformedBinding := *malformedAssessment.HostPackageIdentity
	malformedBinding.DeliveryMappingDigest = "sha256:" + strings.Repeat("c", 64)
	malformedAssessment.HostPackageIdentity = &malformedBinding
	malformed.Assessment = malformedAssessment
	meta.rev = malformed
	response = httptest.NewRecorder()
	server.handleResources(response, request(TargetProfilePlaceFeature))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("malformed stored binding status = %d, want 500: %s", response.Code, response.Body.String())
	}
}
