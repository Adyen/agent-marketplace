package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type placeSearchStore struct {
	*availabilityRouteStore
	rows []store.AssessedResourceRevision
}

func (s *placeSearchStore) SearchResourceRevisions(_ context.Context, filter store.ResourceSearchFilter) ([]store.AssessedResourceRevision, error) {
	rows := slices.Clone(s.rows)
	if filter.Kind != "" {
		rows = slices.DeleteFunc(rows, func(rev store.AssessedResourceRevision) bool {
			return rev.Coordinate.Kind != filter.Kind
		})
	}
	return rows, nil
}

func TestOptedInSearchIncludesBoundPlaceRows(t *testing.T) {
	server, meta, _, _, _ := managedRouteFixture(t)
	server.cfg.Management.Mode = "file"
	legacy := meta.rev
	scoped := boundPlaceRevision(t, meta.rev)
	virtualRevision := meta.rev.ResourceRevision
	virtualRevision.Coordinate.Kind = nameutil.TargetProfileKind
	virtualRevision.Coordinate.Name = "invented-app"
	virtualRevision.ArchiveDigest = ""
	virtualRevision.ContentDigest = store.RevisionDigest("sha256:" + strings.Repeat("9", 64))
	virtual := testAssessedRevision(t, virtualRevision, store.AssessmentDecisionAllow)
	rows := []store.AssessedResourceRevision{legacy, scoped, virtual}
	searchMeta := &placeSearchStore{availabilityRouteStore: meta, rows: rows}
	server.meta = searchMeta

	search := func(path string, features ...string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		for _, feature := range features {
			request.Header.Add(placeFeaturesHeader, feature)
		}
		server.handleSearch(response, request)
		return response
	}
	assertVary := func(response *httptest.ResponseRecorder) {
		t.Helper()
		if !strings.Contains(strings.Join(response.Header().Values("Vary"), ","), placeFeaturesHeader) {
			t.Fatalf("Vary = %q, want %s", response.Header().Values("Vary"), placeFeaturesHeader)
		}
	}
	decode := func(response *httptest.ResponseRecorder) (int, []struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	}) {
		t.Helper()
		var result struct {
			Items []struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"items"`
			Total int `json:"total"`
		}
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result.Total, result.Items
	}

	response := search("/api/v1/search?limit=10", TargetProfilePlaceFeature)
	if response.Code != http.StatusOK {
		t.Fatalf("opted-in search status = %d: %s", response.Code, response.Body.String())
	}
	total, items := decode(response)
	if total != 3 || len(items) != 3 ||
		items[0].Kind != "invented-app/mod" || items[0].Name != "sample" ||
		items[1].Kind != "mcpserver" || items[1].Name != "example" ||
		items[2].Kind != nameutil.TargetProfileKind || items[2].Name != "invented-app" {
		t.Fatalf("opted-in search total/items = %d/%+v, want invented-app/mod, mcpserver/example, target-profile/invented-app", total, items)
	}
	assertVary(response)

	scopedQuery := "/api/v1/search?kind=invented-app%2Fmod"
	response = search(scopedQuery, TargetProfilePlaceFeature)
	if response.Code != http.StatusOK {
		t.Fatalf("scoped kind search status = %d: %s", response.Code, response.Body.String())
	}
	assertVary(response)
	total, items = decode(response)
	if total != 1 || len(items) != 1 || items[0].Kind != "invented-app/mod" || items[0].Name != "sample" {
		t.Fatalf("scoped kind search total/items = %d/%+v, want invented-app/mod/sample", total, items)
	}

	targetProfileQuery := "/api/v1/search?kind=target-profile"
	response = search(targetProfileQuery, TargetProfilePlaceFeature)
	if response.Code != http.StatusOK {
		t.Fatalf("target-profile search status = %d: %s", response.Code, response.Body.String())
	}
	assertVary(response)
	total, items = decode(response)
	if total != 1 || len(items) != 1 || items[0].Kind != nameutil.TargetProfileKind || items[0].Name != "invented-app" {
		t.Fatalf("target-profile search total/items = %d/%+v, want target-profile/invented-app", total, items)
	}

	for _, query := range []string{scopedQuery, targetProfileQuery} {
		for _, features := range [][]string{nil, {TargetProfilePlaceFeature, TargetProfilePlaceFeature}} {
			response = search(query, features...)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("search for %q with features %q status = %d, want 400", query, features, response.Code)
			}
			assertVary(response)
		}
	}

	malformed := scoped
	malformedAssessment := malformed.Assessment
	malformedBinding := *malformedAssessment.HostPackageIdentity
	malformedBinding.DeliveryMappingDigest = "sha256:" + strings.Repeat("c", 64)
	malformedAssessment.HostPackageIdentity = &malformedBinding
	malformed.Assessment = malformedAssessment
	searchMeta.rows = []store.AssessedResourceRevision{legacy, malformed, virtual}
	response = search("/api/v1/search?limit=10", TargetProfilePlaceFeature)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("search with malformed scoped binding status = %d, want 500: %s", response.Code, response.Body.String())
	}
	assertVary(response)

	response = search("/api/v1/search?limit=10")
	if response.Code != http.StatusOK {
		t.Fatalf("legacy search with malformed scoped row status = %d: %s", response.Code, response.Body.String())
	}
	assertVary(response)
	total, items = decode(response)
	if total != 1 || len(items) != 1 || items[0].Kind != "mcpserver" || items[0].Name != "example" {
		t.Fatalf("legacy search total/items = %d/%+v, want mcpserver/example", total, items)
	}

	capabilities := httptest.NewRecorder()
	server.handleCapabilities(capabilities, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
	if capabilities.Code != http.StatusOK {
		t.Fatalf("capabilities status = %d: %s", capabilities.Code, capabilities.Body.String())
	}
	var advertised struct {
		Features      []string `json:"features"`
		ResourceKinds []string `json:"resource_kinds"`
	}
	if err := json.NewDecoder(capabilities.Body).Decode(&advertised); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(advertised.Features, TargetProfilePlaceFeature) {
		t.Fatalf("capability features = %v, want %q", advertised.Features, TargetProfilePlaceFeature)
	}
	if want := []string{"skill", "command", "subagent", "hook", "mcpserver", "collection"}; !slices.Equal(advertised.ResourceKinds, want) {
		t.Fatalf("capability resource kinds = %v, want %v", advertised.ResourceKinds, want)
	}
}

func TestLegacySearchOmitsPlaceRowsBeforePagination(t *testing.T) {
	server, meta, _, _, _ := managedRouteFixture(t)
	scoped := meta.rev
	scoped.Coordinate.Kind = "aaa-app/mod"
	scoped.Coordinate.Name = "scoped"
	scoped.Assessment.Coordinate = scoped.Coordinate
	scoped.Occurrence.Coordinate = scoped.Coordinate
	virtual := meta.rev
	virtual.Coordinate.Kind = nameutil.TargetProfileKind
	virtual.Coordinate.Name = "invented-app"
	virtual.Assessment.Coordinate = virtual.Coordinate
	virtual.Occurrence.Coordinate = virtual.Coordinate
	rows := []store.AssessedResourceRevision{
		scoped,
		virtual,
		meta.rev,
	}
	server.meta = &placeSearchStore{availabilityRouteStore: meta, rows: rows}

	for _, tc := range []struct {
		query  string
		offset int
	}{
		{query: "/api/v1/search?limit=1"},
		{query: "/api/v1/search?limit=1&offset=1", offset: 1},
	} {
		response := httptest.NewRecorder()
		server.handleSearch(response, httptest.NewRequest(http.MethodGet, tc.query, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("search %q status = %d: %s", tc.query, response.Code, response.Body.String())
		}
		var result struct {
			Items []struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"items"`
			Total  int `json:"total"`
			Offset int `json:"offset"`
		}
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result.Total != 1 || result.Offset != tc.offset {
			t.Fatalf("search %q total/offset = %d/%d, want 1/%d", tc.query, result.Total, result.Offset, tc.offset)
		}
		if tc.offset == 0 {
			if len(result.Items) != 1 || result.Items[0].Name != "example" || result.Items[0].Kind != "mcpserver" {
				t.Fatalf("first page items = %+v, want mcpserver/example", result.Items)
			}
		} else if len(result.Items) != 0 {
			t.Fatalf("second page items = %+v, want empty", result.Items)
		}
	}
}
