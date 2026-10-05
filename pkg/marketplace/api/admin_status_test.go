package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type adminAttemptStore struct {
	store.MetadataStore
	store.ManagedConfigurationStore
	store.ManagedRefreshAttemptStore
	query store.ManagedRefreshAttemptQuery
}

func (s *adminAttemptStore) ListManagedRefreshAttempts(_ context.Context, query store.ManagedRefreshAttemptQuery) ([]store.ManagedRefreshAttempt, error) {
	s.query = query
	return []store.ManagedRefreshAttempt{{Sequence: 7, Marketplace: query.Marketplace, Registry: "registry", JobID: "job", Outcome: "failed", Attempt: 2, FailureCode: "publication_conflict/scheduled", FailureCause: "publication returned a conflict"}}, nil
}

func TestAdminAttemptHistoryPassesScopeAndCursor(t *testing.T) {
	s, handler := adminHTTPFixture(t)
	wrapped := &adminAttemptStore{MetadataStore: s.meta, ManagedConfigurationStore: s.meta.(store.ManagedConfigurationStore)}
	s.meta = wrapped
	r := adminAuthRequest("GET")
	r.URL.Path = "/api/v1/admin/attempts"
	r.URL.RawQuery = "limit=1&before=9&registry_id=registry&job_id=job"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("history: %d %s", w.Code, w.Body.String())
	}
	expected := store.ManagedRefreshAttemptQuery{Marketplace: "admin-test", Registry: "registry", JobID: "job", Before: 9, Limit: 1}
	if wrapped.query != expected {
		t.Fatalf("scope: %+v", wrapped.query)
	}
	var body struct {
		Attempts   []store.ManagedRefreshAttempt `json:"attempts"`
		NextBefore int64                         `json:"next_before"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Attempts) != 1 || body.Attempts[0].Outcome != "failed" || body.NextBefore != 7 {
		t.Fatalf("attempt response: %+v", body)
	}
	if body.Attempts[0].FailureCode != "publication_conflict/scheduled" || body.Attempts[0].FailureCause != "publication returned a conflict" {
		t.Fatalf("attempt failure fields not exposed: %+v", body.Attempts[0])
	}
	var raw struct {
		Attempts []map[string]any `json:"attempts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Attempts[0]["failure_code"] != "publication_conflict/scheduled" || raw.Attempts[0]["failure_cause"] != "publication returned a conflict" {
		t.Fatalf("serialized attempt missing failure fields: %v", raw.Attempts[0])
	}
}

func TestAdminAttemptHistoryPagination(t *testing.T) {
	_, handler := adminHTTPFixture(t)
	for _, tc := range []struct {
		query  string
		status int
	}{{"", 200}, {"?limit=0", 400}, {"?limit=101", 400}, {"?before=-1", 400}, {"?before=bad", 400}} {
		r := adminAuthRequest("GET")
		r.URL.Path = "/api/v1/admin/attempts"
		if len(tc.query) > 0 {
			r.URL.RawQuery = tc.query[1:]
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %s", tc.query, w.Code, w.Body.String())
		}
		if tc.status == 200 && w.Body.String() != "{\"attempts\":[],\"next_before\":0}\n" {
			t.Fatalf("empty attempts: %s", w.Body.String())
		}
	}
}

func TestAdminJobIsMarketplaceScoped(t *testing.T) {
	s, handler := adminHTTPFixture(t)
	for _, mp := range []string{"admin-test", "other"} {
		payload, _ := json.Marshal(map[string]any{"marketplace_id": mp, "revision": 1})
		if err := s.meta.EnqueueJob(context.Background(), store.Job{ID: store.JobID(mp), Kind: "managed-reconcile", Payload: payload, IdempotencyKey: mp, MaxAttempts: 3}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		id     string
		status int
	}{{"admin-test", 200}, {"other", 404}} {
		r := adminAuthRequest("GET")
		r.URL.Path = "/api/v1/admin/jobs/" + tc.id
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s status=%d body=%s", tc.id, w.Code, w.Body.String())
		}
	}
}
