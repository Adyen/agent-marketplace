package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testAdminAuth() AdminAuthOptions {
	return AdminAuthOptions{TrustedProxies: []string{"127.0.0.1/32"}, Origin: "https://marketplace.test", Issuer: "test-sso", SubjectHeader: "X-Test-Subject", GroupsHeader: "X-Test-Groups", AdminGroups: []string{"marketplace-admins"}}
}

func adminAuthRequest(method string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1/admin/configuration", strings.NewReader(`{}`))
	r.RemoteAddr = "127.0.0.1:4567"
	r.Header.Set("X-Test-Subject", "operator")
	r.Header.Set("X-Test-Groups", `["marketplace-admins"]`)
	if method != http.MethodGet {
		r.Header.Set("Origin", "https://marketplace.test")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Marketplace-Admin", "1")
	}
	return r
}

func TestAdminAuthorizationBoundary(t *testing.T) {
	for _, localHTTP := range []bool{false, true} {
		t.Run(map[bool]string{false: "https", true: "local-http"}[localHTTP], func(t *testing.T) {
			testAdminAuthorizationBoundary(t, localHTTP)
		})
	}
}

func testAdminAuthorizationBoundary(t *testing.T, localHTTP bool) {
	t.Helper()
	options := testAdminAuth()
	if localHTTP {
		options.Origin = "http://localhost:8080"
		options.AllowInsecureLocalhost = true
	}
	for _, tc := range []struct {
		name   string
		method string
		mutate func(*http.Request)
		want   int
	}{
		{"authorized read", "GET", func(*http.Request) {}, 200},
		{"authorized write", "POST", func(*http.Request) {}, 200},
		{"missing identity", "GET", func(r *http.Request) { r.Header.Del("X-Test-Subject") }, 401},
		{"ordinary user", "GET", func(r *http.Request) { r.Header.Set("X-Test-Groups", `["catalog-users"]`) }, 403},
		{"direct bypass", "GET", func(r *http.Request) { r.RemoteAddr = "192.0.2.10:4567"; r.Header.Set("X-Forwarded-For", "127.0.0.1") }, 403},
		{"duplicate subject", "GET", func(r *http.Request) { r.Header.Add("X-Test-Subject", "other") }, 401},
		{"duplicate groups", "GET", func(r *http.Request) { r.Header.Add("X-Test-Groups", `["marketplace-admins"]`) }, 401},
		{"malformed groups", "GET", func(r *http.Request) { r.Header.Set("X-Test-Groups", "marketplace-admins") }, 401},
		{"cross-origin read", "GET", func(r *http.Request) { r.Header.Set("Origin", "https://other.test") }, 403},
		{"missing origin", "POST", func(r *http.Request) { r.Header.Del("Origin") }, 403},
		{"cross-origin write", "POST", func(r *http.Request) { r.Header.Set("Origin", "https://other.test") }, 403},
		{"missing custom header", "POST", func(r *http.Request) { r.Header.Del("X-Marketplace-Admin") }, 403},
		{"form mutation", "POST", func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }, 415},
		{"cross-origin preflight", "OPTIONS", func(r *http.Request) { r.Header.Set("Origin", "https://other.test") }, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, err := newAdminGuard(options, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				actor, ok := adminActor(r.Context())
				if !ok || actor.Subject != "operator" || actor.Issuer != "test-sso" {
					t.Error("authorized handler has no validated actor")
				}
				w.WriteHeader(http.StatusOK)
			}))
			if err != nil {
				t.Fatal(err)
			}
			r := adminAuthRequest(tc.method)
			if tc.method != http.MethodGet {
				r.Header.Set("Origin", options.Origin)
			}
			tc.mutate(r)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private response can be cached")
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("admin CORS enabled")
			}
		})
	}
}

func TestAdminHandlerLocalHTTP(t *testing.T) {
	s, _ := adminHTTPFixture(t)
	s.cfg.Admin.Origin = "http://localhost:8080"
	s.cfg.Admin.AllowInsecureLocalhost = true
	r := adminAuthRequest(http.MethodGet)
	r.URL.Path = "/api/v1/admin/session"
	r.Header.Set("Origin", "http://localhost:8080")
	w := httptest.NewRecorder()
	s.adminHandler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminGuardBoundsRequestsAndExpensivePreviews(t *testing.T) {
	for _, tc := range []struct {
		path    string
		allowed int
	}{{"/api/v1/admin/configuration", 60}, {"/api/v1/admin/previews", 5}} {
		t.Run(tc.path, func(t *testing.T) {
			handler, err := newAdminGuard(testAdminAuth(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i <= tc.allowed; i++ {
				r := adminAuthRequest("POST")
				r.URL.Path = tc.path
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				want := 200
				if i == tc.allowed {
					want = 429
				}
				if w.Code != want {
					t.Fatalf("request %d: got %d want %d", i, w.Code, want)
				}
			}
		})
	}
}

func TestAdminGuardRejectsMutationHeadersAsIdentity(t *testing.T) {
	for _, header := range []string{"If-Match", "Idempotency-Key", "Referer", "Upgrade", "Expect", "TE", "Trailer", "Traceparent", "Set-Cookie"} {
		cfg := testAdminAuth()
		cfg.SubjectHeader = header
		if _, err := newAdminGuard(cfg, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})); err == nil {
			t.Errorf("request-control header %s accepted as identity", header)
		}
	}
}

func TestAdminGuardRejectsIncompleteTrustConfiguration(t *testing.T) {
	for _, mutate := range []func(*AdminAuthOptions){
		func(c *AdminAuthOptions) { c.TrustedProxies = nil },
		func(c *AdminAuthOptions) { c.TrustedProxies = []string{"0.0.0.0/0"} },
		func(c *AdminAuthOptions) { c.Origin = "https://marketplace.test/path" },
		func(c *AdminAuthOptions) { c.AdminGroups = nil },
		func(c *AdminAuthOptions) { c.SubjectHeader = "X-Forwarded-For" },
		func(c *AdminAuthOptions) { c.GroupsHeader = c.SubjectHeader },
	} {
		cfg := testAdminAuth()
		mutate(&cfg)
		if _, err := newAdminGuard(cfg, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})); err == nil {
			t.Fatal("unsafe administration trust configuration accepted")
		}
	}
}
