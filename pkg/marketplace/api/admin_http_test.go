package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

func adminHTTPFixture(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	meta := sqlite.New(":memory:")
	if err := meta.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	cfg := config.Config{Instance: config.Instance{ID: "admin-test", Name: "Test", CanonicalURL: "https://marketplace.test"}, Management: config.Management{Mode: "database"}, Policy: config.Policy{LicenseAllowlist: []string{"MIT"}, LicenseUndeclared: "reject"}, Admin: config.Administration{Enabled: true, TrustedProxies: []string{"127.0.0.1/32"}, Origin: "https://marketplace.test", Issuer: "test-sso", SubjectHeader: "X-Test-Subject", GroupsHeader: "X-Test-Groups", AdminGroups: []string{"marketplace-admins"}}}
	svc := admin.Service{Metadata: meta, Deployment: cfg.Management}
	if _, err := svc.Adopt(context.Background(), cfg, admin.AdoptionRequest{Actor: store.ManagedActor{Issuer: "test-sso", Subject: "operator"}, Reason: "Adopt test", RequestID: "adopt", IdempotencyKey: "adopt"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg, meta: meta, mpID: "admin-test"}
	return s, s.adminHandler()
}

func TestAdminHTTPValidationAndApply(t *testing.T) {
	_, handler := adminHTTPFixture(t)
	body := `{"action":"set_policy","license_policy":{"allowlist":["MIT","Apache-2.0"],"undeclared":"reject"},"reason":"Allow approved licenses"}`
	request := func(path, version, key, payload string) *httptest.ResponseRecorder {
		r := adminAuthRequest("POST")
		r.URL.Path = "/api/v1/admin/" + path
		r.Body = http.NoBody
		if payload != "" {
			r.Body = httptest.NewRequest("POST", "/", strings.NewReader(payload)).Body
		}
		if version != "" {
			r.Header.Set("If-Match", version)
		}
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request("validate", `"1"`, "", body); w.Code != 200 {
		t.Fatalf("validate: %d %s", w.Code, w.Body.String())
	} else if got := strings.TrimSpace(w.Body.String()); got != `{"valid":true,"warnings":[]}` {
		t.Fatalf("static validation must not claim preview state, which only the discovery preview knows: %s", got)
	}
	if w := request("changes", "", "missing-version", body); w.Code != 428 {
		t.Fatalf("missing version: %d %s", w.Code, w.Body.String())
	}
	if w := request("changes", `"1"`, "", body); w.Code != 400 {
		t.Fatalf("missing idempotency: %d %s", w.Code, w.Body.String())
	}
	w := request("changes", `"1"`, "policy-change", body)
	if w.Code != 202 {
		t.Fatalf("apply: %d %s", w.Code, w.Body.String())
	}
	var accepted store.ManagedChange
	if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.Revision != 2 || accepted.Actor.Subject != "operator" {
		t.Fatalf("accepted: %+v", accepted)
	}
	if w := request("changes", `"1"`, "policy-change", body); w.Code != 202 {
		t.Fatalf("retry: %d %s", w.Code, w.Body.String())
	}
	if w := request("changes", `"1"`, "stale", body); w.Code != 409 {
		t.Fatalf("stale: %d %s", w.Code, w.Body.String())
	}
	if w := request("changes", `"2"`, "unknown-fields", strings.TrimSuffix(body, "}")+`,"actor":{"subject":"forged"}}`); w.Code != 400 {
		t.Fatalf("forged actor: %d %s", w.Code, w.Body.String())
	}
	if w := request("changes", `"2"`, "trailing-json", body+`{}`); w.Code != 400 {
		t.Fatalf("trailing JSON: %d %s", w.Code, w.Body.String())
	}
}

func TestAdminIdentityConflictIsNotPersistenceFailure(t *testing.T) {
	w := httptest.NewRecorder()
	writeAdminServiceError(w, store.ErrManagedIdentityConflict)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"identity_conflict"`) {
		t.Fatalf("identity collision reported as infrastructure failure: %d %s", w.Code, w.Body.String())
	}
}

func TestAdminInvalidConfigurationCarriesFieldDetail(t *testing.T) {
	w := httptest.NewRecorder()
	writeAdminServiceError(w, &admin.ValidationError{Message: `slug "a/b" is invalid`, Fields: map[string]string{"slug": `"a/b" is invalid`}})
	var body struct {
		Code, Message string
		Fields        map[string]string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("decode %d %s: %v", w.Code, w.Body.String(), err)
	}
	if body.Code != "invalid_configuration" || body.Message != `slug "a/b" is invalid` || body.Fields["slug"] != `"a/b" is invalid` {
		t.Fatalf("detail lost: %+v", body)
	}
	w = httptest.NewRecorder()
	writeAdminServiceError(w, admin.ErrInvalidConfiguration)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `"code":"invalid_configuration"`) || strings.Contains(w.Body.String(), `"fields"`) {
		t.Fatalf("bare sentinel must keep the generic envelope: %d %s", w.Code, w.Body.String())
	}
}

func TestAdminHTTPRegistrationStaysDisabledByDefault(t *testing.T) {
	meta := sqlite.New(":memory:")
	if err := meta.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = meta.Close() }()
	s := NewServer(config.Config{Instance: config.Instance{ID: "test"}}, meta, nil)
	w := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(w, adminAuthRequest("GET"))
	if w.Code != 404 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unregistered or enabled administration: %d, %s", w.Code, w.Header().Get("Cache-Control"))
	}
}

func TestAdminHTTPPrivateReadsAndDisabledRoutes(t *testing.T) {
	s, handler := adminHTTPFixture(t)
	for _, route := range []string{"session", "configuration", "activity", "instance", "profiles"} {
		r := adminAuthRequest("GET")
		r.URL.Path = "/api/v1/admin/" + route
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %s", route, w.Code, w.Body.String())
		}
	}
	s.cfg.Admin.Enabled = false
	w := httptest.NewRecorder()
	s.adminHandler().ServeHTTP(w, adminAuthRequest("GET"))
	if w.Code != 404 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("disabled: %d", w.Code)
	}
}
