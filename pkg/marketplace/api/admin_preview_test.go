package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestAdminHTTPPreviewRequestAndRead(t *testing.T) {
	s, _ := adminHTTPFixture(t)
	s.cfg.Management.AllowedOrigins = []string{"https://git.test"}
	handler := s.adminHandler()
	proposal := `{"action":"upsert_registry","registry_id":"draft","registry":{"id":"draft","slug":"draft","origin":"https://git.test/org/repo","ref":"main","roots":[{"path":"skills"}],"acquisition":"index-only","publisher":{},"license":"","aliases":[],"historical_origins":[],"generation":0,"lifecycle":"active","paused":false,"withdrawn":false,"retired":false},"reason":""}`
	post := func(version, payload string) *httptest.ResponseRecorder {
		r := adminAuthRequest("POST")
		r.URL.Path = "/api/v1/admin/previews"
		r.Body = httptest.NewRequest("POST", "/", strings.NewReader(payload)).Body
		if version != "" {
			r.Header.Set("If-Match", version)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := post("", proposal); w.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing revision: %d %s", w.Code, w.Body.String())
	}
	if w := post(`"1"`, `{"action":"pause","registry_id":"x","reason":""}`); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-upsert preview: %d %s", w.Code, w.Body.String())
	}
	w := post(`"1"`, proposal)
	if w.Code != http.StatusAccepted || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	var preview store.ManagedPreview
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Status != store.ManagedPreviewPending || preview.Actor.Subject != "operator" || preview.Existing || preview.BaseRevision != 1 {
		t.Fatalf("preview payload: %+v", preview)
	}
	if strings.Contains(w.Body.String(), `"completed_at"`) {
		t.Fatalf("pending preview must not report completion: %s", w.Body.String())
	}
	if job, err := s.meta.GetJob(t.Context(), preview.JobID); err != nil || job.Kind != "managed-preview" {
		t.Fatalf("preview job: %+v %v", job, err)
	}

	get := func(id string) *httptest.ResponseRecorder {
		r := adminAuthRequest("GET")
		r.URL.Path = "/api/v1/admin/previews/" + id
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := get(preview.ID); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"`+preview.ID+`"`) {
		t.Fatalf("read preview: %d %s", w.Code, w.Body.String())
	}
	if w := get("missing"); w.Code != http.StatusNotFound {
		t.Fatalf("missing preview: %d %s", w.Code, w.Body.String())
	}
	if w := get(preview.ID + "/extra"); w.Code != http.StatusNotFound {
		t.Fatalf("nested path: %d %s", w.Code, w.Body.String())
	}
	if after, _ := s.meta.(store.ManagedConfigurationStore).CurrentManagedConfiguration(t.Context(), "admin-test"); after.Revision != 1 {
		t.Fatal("preview request must not change configuration")
	}
}
