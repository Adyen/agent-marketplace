package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type placeSearchStore struct {
	*availabilityRouteStore
	rows []store.AssessedResourceRevision
}

func (s *placeSearchStore) SearchResourceRevisions(context.Context, store.ResourceSearchFilter) ([]store.AssessedResourceRevision, error) {
	return slices.Clone(s.rows), nil
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
