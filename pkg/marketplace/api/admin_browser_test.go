package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
)

func browserAdminRequest(method, target, cookie string, headers map[string]string) *http.Request {
	r := adminRequest(method, target, cookie)
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	return r
}

func assertAdminBrowserRedirect(t *testing.T, w *httptest.ResponseRecorder, wantCode, wantNext string) {
	t.Helper()
	if w.Code != http.StatusSeeOther {
		t.Fatalf("browser failure status: %d body=%s", w.Code, w.Body.String())
	}
	location := w.Header().Get("Location")
	target, err := url.Parse(location)
	if err != nil || target.IsAbs() || target.Host != "" || target.Path != "/marketplace/auth/error" {
		t.Fatalf("browser failure location %q", location)
	}
	query := target.Query()
	for key := range query {
		if key != "code" && key != "next" {
			t.Fatalf("unexpected error-page parameter %q in %q", key, location)
		}
	}
	if len(query["code"]) != 1 || query.Get("code") != wantCode {
		t.Fatalf("browser failure code: %q want %q", location, wantCode)
	}
	if wantNext == "" {
		if _, ok := query["next"]; ok {
			t.Fatalf("unsafe destination propagated: %q", location)
		}
	} else if query.Get("next") != wantNext || len(query["next"]) != 1 {
		t.Fatalf("validated destination: %q want %q", location, wantNext)
	}
	if w.Header().Get("Referrer-Policy") != "no-referrer" ||
		!strings.Contains(w.Header().Get("Cache-Control"), "no-store") ||
		!strings.Contains(w.Header().Get("X-Robots-Tag"), "noindex") ||
		w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("browser failure headers: %v", w.Header())
	}
	assertAdminNoSession(t, w)
}

func assertAdminNoSession(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == testSecureSessionCookie && cookie.Value != "" {
			t.Fatalf("session cookie issued on failed sign-in: %+v", cookie)
		}
	}
}

func assertAdminJSONError(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, wantCode, wantMessage string) {
	t.Helper()
	if w.Code != wantStatus || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("machine failure: %d %s content-type=%q", w.Code, w.Body.String(), w.Header().Get("Content-Type"))
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode admin error: %v: %s", err, w.Body.String())
	}
	if body.Code != wantCode {
		t.Fatalf("machine failure code %q want %q", body.Code, wantCode)
	}
	if wantMessage != "" && body.Message != wantMessage {
		t.Fatalf("machine failure message %q want %q", body.Message, wantMessage)
	}
	assertAdminNoSession(t, w)
}

func assertAdminLoginCookieConsumption(t *testing.T, w *httptest.ResponseRecorder, consumed bool) {
	t.Helper()
	expired := false
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == testSecureLoginCookie && cookie.MaxAge < 0 {
			expired = true
		}
	}
	if expired != consumed {
		t.Fatalf("login cookie consumption = %v, want %v; cookies %v", expired, consumed, w.Result().Cookies())
	}
}

// spec: marketplace-admin-auth/browser-and-machine-clients-receive-appropriate-failure-responses
// spec: marketplace-admin-auth/failed-authentication-does-not-leak-diagnostics-or-issue-a-session
func TestAdminOIDCBrowserErrorHandoff(t *testing.T) {
	type attempt struct {
		target  string
		cookie  string
		headers map[string]string
	}
	cases := []struct {
		name          string
		setup         func(t *testing.T, issuer *testOIDCIssuer, handler http.Handler) attempt
		wantStatus    int
		wantCode      string
		wantMessage   string
		wantNext      string
		loginConsumed bool
		sensitive     []string
		verifyIssuer  func(t *testing.T, issuer *testOIDCIssuer)
	}{
		{
			name: "login discovery outage carries validated next",
			setup: func(t *testing.T, issuer *testOIDCIssuer, _ http.Handler) attempt {
				issuer.discoveryCode = http.StatusServiceUnavailable
				return attempt{target: "/api/v1/admin/auth/login?next=/marketplace/admin/registries/team"}
			},
			wantStatus: http.StatusServiceUnavailable, wantCode: "authentication_unavailable",
			wantMessage: "The identity provider is unavailable.",
			wantNext:    "/marketplace/admin/registries/team",
		},
		{
			name: "login rejects unsafe next",
			setup: func(_ *testing.T, _ *testOIDCIssuer, _ http.Handler) attempt {
				return attempt{target: "/api/v1/admin/auth/login?next=https://evil.test/phish"}
			},
			wantStatus: http.StatusBadRequest, wantCode: "authentication_request_invalid",
			wantMessage: "The sign-in destination is not an administration path.",
			sensitive:   []string{"evil.test"},
		},
		{
			name: "login rejects foreign origin",
			setup: func(_ *testing.T, _ *testOIDCIssuer, _ http.Handler) attempt {
				return attempt{target: "/api/v1/admin/auth/login", headers: map[string]string{"Origin": "https://evil.test"}}
			},
			wantStatus: http.StatusForbidden, wantCode: "origin_rejected",
			wantMessage: "Use the configured same-origin administration interface.",
			sensitive:   []string{"evil.test"},
		},
		{
			name: "callback rejects missing transaction",
			setup: func(_ *testing.T, _ *testOIDCIssuer, _ http.Handler) attempt {
				return attempt{target: "/api/v1/admin/auth/callback?state=x&code=c"}
			},
			wantStatus: http.StatusUnauthorized, wantCode: "authentication_rejected",
			wantMessage: "Sign-in could not be completed.",
		},
		{
			name: "callback rejects foreign origin",
			setup: func(t *testing.T, _ *testOIDCIssuer, handler http.Handler) attempt {
				login, transaction := startOIDCLogin(t, handler)
				return attempt{
					target:  "/api/v1/admin/auth/callback?state=" + transaction.State + "&code=c",
					cookie:  testSecureLoginCookie + "=" + cookieValue(t, login, testSecureLoginCookie),
					headers: map[string]string{"Origin": "https://evil.test"},
				}
			},
			wantStatus: http.StatusForbidden, wantCode: "origin_rejected",
			wantMessage: "Use the configured same-origin administration interface.",
			sensitive:   []string{"evil.test"},
		},
		{
			name: "callback ignores raw next and carries only the signed destination",
			setup: func(t *testing.T, _ *testOIDCIssuer, handler http.Handler) attempt {
				login := httptest.NewRecorder()
				handler.ServeHTTP(login, adminRequest(http.MethodGet, "/api/v1/admin/auth/login?next=/marketplace/admin/registries/team", ""))
				if login.Code != http.StatusSeeOther {
					t.Fatalf("login: %d %s", login.Code, login.Body.String())
				}
				return attempt{
					target: "/api/v1/admin/auth/callback?error=access_denied&next=https://evil.test/phish",
					cookie: testSecureLoginCookie + "=" + cookieValue(t, login, testSecureLoginCookie),
				}
			},
			wantStatus: http.StatusUnauthorized, wantCode: "authentication_rejected",
			wantMessage: "Sign-in could not be completed.",
			wantNext:    "/marketplace/admin/registries/team", loginConsumed: true,
			sensitive: []string{"evil.test"},
		},
		{
			name: "callback rejects expired transaction",
			setup: func(t *testing.T, _ *testOIDCIssuer, _ http.Handler) attempt {
				now := time.Now().Unix()
				expired := sealAdminPayload(t, adminLoginTransaction{
					Version: 1, State: adminRandom(), Nonce: adminRandom(), Verifier: "verifier",
					Next: "/marketplace/admin", IssuedAt: now - 3600, Expiry: now - 1800,
				})
				return attempt{
					target: "/api/v1/admin/auth/callback?state=x&code=c",
					cookie: testSecureLoginCookie + "=" + expired,
				}
			},
			wantStatus: http.StatusUnauthorized, wantCode: "authentication_rejected",
			wantMessage: "Sign-in could not be completed.",
		},
		{
			name: "callback maps provider denial without diagnostics",
			setup: func(t *testing.T, _ *testOIDCIssuer, handler http.Handler) attempt {
				login, _ := startOIDCLogin(t, handler)
				return attempt{
					target:  "/api/v1/admin/auth/callback?error=access_denied&error_description=topsecret-provider-detail",
					cookie:  testSecureLoginCookie + "=" + cookieValue(t, login, testSecureLoginCookie),
					headers: map[string]string{},
				}
			},
			wantStatus: http.StatusUnauthorized, wantCode: "authentication_rejected",
			wantMessage: "Sign-in could not be completed.",
			wantNext:    "/marketplace/admin", loginConsumed: true,
			sensitive: []string{"access_denied", "topsecret-provider-detail"},
		},
		{
			name: "callback rejects wrong state",
			setup: func(t *testing.T, _ *testOIDCIssuer, handler http.Handler) attempt {
				login, _ := startOIDCLogin(t, handler)
				return attempt{
					target: "/api/v1/admin/auth/callback?state=forged-state&code=c",
					cookie: testSecureLoginCookie + "=" + cookieValue(t, login, testSecureLoginCookie),
				}
			},
			wantStatus: http.StatusUnauthorized, wantCode: "authentication_rejected",
			wantMessage: "Sign-in could not be completed.",
			wantNext:    "/marketplace/admin", loginConsumed: true,
			sensitive: []string{"forged-state"},
		},
		{
			name: "callback rejects malformed provider response",
			setup: func(t *testing.T, _ *testOIDCIssuer, handler http.Handler) attempt {
				login, transaction := startOIDCLogin(t, handler)
				return attempt{
					target: "/api/v1/admin/auth/callback?state=" + transaction.State,
					cookie: testSecureLoginCookie + "=" + cookieValue(t, login, testSecureLoginCookie),
				}
			},
			wantStatus: http.StatusBadRequest, wantCode: "authentication_request_invalid",
			wantMessage: "The identity provider response is invalid.",
			wantNext:    "/marketplace/admin", loginConsumed: true,
		},
		{
			name: "callback rejects invalid grant",
			setup: func(t *testing.T, issuer *testOIDCIssuer, handler http.Handler) attempt {
				login, transaction := startOIDCLogin(t, handler)
				issuer.tokenCode = http.StatusBadRequest
				issuer.tokenBody = []byte(`{"error":"invalid_grant","error_description":"secret-grant-detail"}`)
				return attempt{
					target: "/api/v1/admin/auth/callback?state=" + transaction.State + "&code=stolen-code",
					cookie: testSecureLoginCookie + "=" + cookieValue(t, login, testSecureLoginCookie),
				}
			},
			wantStatus: http.StatusUnauthorized, wantCode: "authentication_rejected",
			wantMessage: "Sign-in could not be completed.",
			wantNext:    "/marketplace/admin", loginConsumed: true,
			sensitive: []string{"secret-grant-detail", "stolen-code"},
			verifyIssuer: func(t *testing.T, issuer *testOIDCIssuer) {
				t.Helper()
				if issuer.tokenCalls == 0 {
					t.Fatal("invalid grant case never reached the token endpoint")
				}
			},
		},
		{
			name: "callback reports token endpoint outage",
			setup: func(t *testing.T, issuer *testOIDCIssuer, handler http.Handler) attempt {
				login, transaction := startOIDCLogin(t, handler)
				issuer.server.Close()
				return attempt{
					target: "/api/v1/admin/auth/callback?state=" + transaction.State + "&code=c",
					cookie: testSecureLoginCookie + "=" + cookieValue(t, login, testSecureLoginCookie),
				}
			},
			wantStatus: http.StatusServiceUnavailable, wantCode: "authentication_unavailable",
			wantMessage: "The identity provider is unavailable.",
			wantNext:    "/marketplace/admin", loginConsumed: true,
		},
		{
			name: "callback reports JWKS outage",
			setup: func(t *testing.T, issuer *testOIDCIssuer, handler http.Handler) attempt {
				login, transaction := startOIDCLogin(t, handler)
				issuer.claims = map[string]any{"nonce": transaction.Nonce, "groups": []string{"marketplace-admins"}}
				issuer.jwksCode = http.StatusServiceUnavailable
				return attempt{
					target: "/api/v1/admin/auth/callback?state=" + transaction.State + "&code=c",
					cookie: testSecureLoginCookie + "=" + cookieValue(t, login, testSecureLoginCookie),
				}
			},
			wantStatus: http.StatusServiceUnavailable, wantCode: "authentication_unavailable",
			wantMessage: "The identity provider is unavailable.",
			wantNext:    "/marketplace/admin", loginConsumed: true,
			verifyIssuer: func(t *testing.T, issuer *testOIDCIssuer) {
				t.Helper()
				if issuer.tokenCalls == 0 {
					t.Fatal("JWKS outage case never reached the token endpoint")
				}
			},
		},
		{
			name: "callback rejects invalid token claim",
			setup: func(t *testing.T, issuer *testOIDCIssuer, handler http.Handler) attempt {
				login, transaction := startOIDCLogin(t, handler)
				issuer.claims = map[string]any{"nonce": "forged-nonce-claim", "groups": []string{"marketplace-admins"}}
				return attempt{
					target: "/api/v1/admin/auth/callback?state=" + transaction.State + "&code=c",
					cookie: testSecureLoginCookie + "=" + cookieValue(t, login, testSecureLoginCookie),
				}
			},
			wantStatus: http.StatusUnauthorized, wantCode: "authentication_rejected",
			wantMessage: "Sign-in could not be completed.",
			wantNext:    "/marketplace/admin", loginConsumed: true,
			sensitive: []string{"forged-nonce-claim"},
			verifyIssuer: func(t *testing.T, issuer *testOIDCIssuer) {
				t.Helper()
				if issuer.tokenCalls == 0 {
					t.Fatal("invalid token claim case never reached the token endpoint")
				}
			},
		},
		{
			name: "callback forbids verified unauthorized identity",
			setup: func(t *testing.T, issuer *testOIDCIssuer, handler http.Handler) attempt {
				login, transaction := startOIDCLogin(t, handler)
				issuer.claims = map[string]any{"nonce": transaction.Nonce, "groups": []string{"other-group"}, "sub": "intruder-subject"}
				return attempt{
					target: "/api/v1/admin/auth/callback?state=" + transaction.State + "&code=c",
					cookie: testSecureLoginCookie + "=" + cookieValue(t, login, testSecureLoginCookie),
				}
			},
			wantStatus: http.StatusForbidden, wantCode: "forbidden",
			wantMessage: "Marketplace administration permission is required.",
			wantNext:    "/marketplace/admin", loginConsumed: true,
			sensitive: []string{"intruder-subject"},
			verifyIssuer: func(t *testing.T, issuer *testOIDCIssuer) {
				t.Helper()
				if issuer.tokenCalls == 0 {
					t.Fatal("unauthorized identity case never reached the token endpoint")
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issuer := newTestOIDCIssuer(t)
			_, handler := adminOIDCFixture(t, issuer, nil)

			for _, accept := range []string{"", "*/*", "application/json"} {
				machine := tc.setup(t, issuer, handler)
				if machine.headers == nil {
					machine.headers = map[string]string{}
				}
				if accept != "" {
					machine.headers["Accept"] = accept
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, browserAdminRequest(http.MethodGet, machine.target, machine.cookie, machine.headers))
				assertAdminJSONError(t, w, tc.wantStatus, tc.wantCode, tc.wantMessage)
				assertAdminLoginCookieConsumption(t, w, tc.loginConsumed)
				for _, leak := range tc.sensitive {
					if strings.Contains(w.Body.String(), leak) {
						t.Fatalf("sensitive value %q in machine body %s", leak, w.Body.String())
					}
				}
				if tc.verifyIssuer != nil {
					tc.verifyIssuer(t, issuer)
				}
			}

			browser := tc.setup(t, issuer, handler)
			if browser.headers == nil {
				browser.headers = map[string]string{}
			}
			browser.headers["Accept"] = "text/html"
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, browserAdminRequest(http.MethodGet, browser.target, browser.cookie, browser.headers))
			assertAdminBrowserRedirect(t, w, tc.wantCode, tc.wantNext)
			assertAdminLoginCookieConsumption(t, w, tc.loginConsumed)
			location := w.Header().Get("Location")
			for _, leak := range tc.sensitive {
				if strings.Contains(location, leak) || strings.Contains(w.Body.String(), leak) {
					t.Fatalf("sensitive value %q propagated to %q", leak, location)
				}
			}
			if tc.verifyIssuer != nil {
				tc.verifyIssuer(t, issuer)
			}
		})
	}
}

// spec: marketplace-admin-auth/browser-and-machine-clients-receive-appropriate-failure-responses
// spec: marketplace-admin-auth/recovery-starts-a-new-login-with-a-safe-destination
// spec: marketplace-admin-auth/root-mounted-administration-uses-root-browser-routes
// spec: marketplace-admin-auth/subpath-and-absent-browser-bases-retain-their-route-contracts
func TestAdminOIDCBrowserRoutesFollowBrowserBase(t *testing.T) {
	for _, tc := range []struct {
		name        string
		browserBase string
		errorPath   string
		adminPath   string
	}{
		{name: "legacy fallback", errorPath: "/marketplace/auth/error", adminPath: "/marketplace/admin"},
		{name: "root", browserBase: "https://marketplace.test", errorPath: "/auth/error", adminPath: "/admin"},
		{name: "trimmed root", browserBase: " https://marketplace.test ", errorPath: "/auth/error", adminPath: "/admin"},
		{name: "subpath", browserBase: "https://marketplace.test/tools/", errorPath: "/tools/auth/error", adminPath: "/tools/admin"},
		{name: "unsafe path falls back", browserBase: "https://marketplace.test//evil", errorPath: "/marketplace/auth/error", adminPath: "/marketplace/admin"},
		{name: "dot path falls back", browserBase: "https://marketplace.test/tools/../evil", errorPath: "/marketplace/auth/error", adminPath: "/marketplace/admin"},
		{name: "encoded dot path falls back", browserBase: "https://marketplace.test/tools/%2e%2e/evil", errorPath: "/marketplace/auth/error", adminPath: "/marketplace/admin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issuer := newTestOIDCIssuer(t)
			issuer.discoveryCode = http.StatusServiceUnavailable
			server, handler := adminOIDCFixtureWithBrowserBase(t, issuer, tc.browserBase, nil)
			next := tc.adminPath + "/registries/team"
			target := "/api/v1/admin/auth/login?next=" + url.QueryEscape(next)
			machine := httptest.NewRecorder()
			handler.ServeHTTP(machine, browserAdminRequest(http.MethodGet, target, "", map[string]string{"Accept": "application/json"}))
			assertAdminJSONError(t, machine, http.StatusServiceUnavailable, "authentication_unavailable", "The identity provider is unavailable.")

			for _, requestTarget := range []string{target, "/api/v1/admin/auth/login"} {
				request := browserAdminRequest(http.MethodGet, requestTarget, "", map[string]string{"Accept": "text/html"})
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)

				if response.Code != http.StatusSeeOther {
					t.Fatalf("status = %d, want 303: %s", response.Code, response.Body.String())
				}
				location, err := url.Parse(response.Header().Get("Location"))
				if err != nil || location.IsAbs() || location.Path != tc.errorPath {
					t.Fatalf("location = %q, want same-origin %q", response.Header().Get("Location"), tc.errorPath)
				}
				wantNext := next
				if requestTarget != target {
					wantNext = tc.adminPath
				}
				if location.Query().Get("code") != "authentication_unavailable" || location.Query().Get("next") != wantNext {
					t.Fatalf("location query = %q", location.RawQuery)
				}
			}

			if tc.adminPath == "/admin" {
				legacyTarget := "/api/v1/admin/auth/login?next=" + url.QueryEscape("/marketplace/admin/registries/team")
				machine := httptest.NewRecorder()
				handler.ServeHTTP(machine, browserAdminRequest(http.MethodGet, legacyTarget, "", map[string]string{"Accept": "application/json"}))
				assertAdminJSONError(t, machine, http.StatusBadRequest, "authentication_request_invalid", "The sign-in destination is not an administration path.")

				browser := httptest.NewRecorder()
				handler.ServeHTTP(browser, browserAdminRequest(http.MethodGet, legacyTarget, "", map[string]string{"Accept": "text/html"}))
				location, err := url.Parse(browser.Header().Get("Location"))
				if browser.Code != http.StatusSeeOther || err != nil || location.Path != tc.errorPath || location.Query().Get("code") != "authentication_request_invalid" || location.Query().Has("next") {
					t.Fatalf("legacy destination response = %d %q", browser.Code, browser.Header().Get("Location"))
				}

				authenticator, err := newAdminOIDCAuthenticatorWithBrowserBase(server.cfg.Admin, tc.browserBase)
				if err != nil {
					t.Fatal(err)
				}
				now := time.Now()
				transaction := adminLoginTransaction{Version: 1, State: adminRandom(), Nonce: adminRandom(), Verifier: adminRandom(), Next: "/marketplace/admin/registries/team", IssuedAt: now.Unix(), Expiry: now.Add(adminLoginTTL).Unix()}
				sealed, err := authenticator.seal(transaction)
				if err != nil {
					t.Fatal(err)
				}
				callback := httptest.NewRecorder()
				callbackTarget := "/api/v1/admin/auth/callback?state=" + transaction.State + "&code=test"
				handler.ServeHTTP(callback, browserAdminRequest(http.MethodGet, callbackTarget, authenticator.cookieName(adminLoginCookie)+"="+sealed, map[string]string{"Accept": "text/html"}))
				location, err = url.Parse(callback.Header().Get("Location"))
				if callback.Code != http.StatusSeeOther || err != nil || location.Path != tc.errorPath || location.Query().Get("code") != "authentication_rejected" || location.Query().Has("next") {
					t.Fatalf("legacy callback response = %d %q", callback.Code, callback.Header().Get("Location"))
				}
			}
		})
	}
}

// spec: marketplace-admin-auth/browser-and-machine-clients-receive-appropriate-failure-responses
func TestAdminOIDCBrowserAcceptNegotiation(t *testing.T) {
	issuer := newTestOIDCIssuer(t)
	_, handler := adminOIDCFixture(t, issuer, nil)
	for _, tc := range []struct {
		name   string
		accept []string
		want   int
	}{
		{"absent", nil, http.StatusUnauthorized},
		{"wildcard only", []string{"*/*"}, http.StatusUnauthorized},
		{"html", []string{"text/html"}, http.StatusSeeOther},
		{"html q zero", []string{"text/html;q=0"}, http.StatusUnauthorized},
		{"html q zero with wildcard", []string{"text/html;q=0, */*"}, http.StatusUnauthorized},
		{"html and json tied", []string{"text/html, application/json"}, http.StatusUnauthorized},
		{"json preferred", []string{"application/json;q=1, text/html;q=0.9"}, http.StatusUnauthorized},
		{"html preferred", []string{"text/html;q=1, application/json;q=0.9"}, http.StatusSeeOther},
		{"wildcard weaker than html", []string{"text/html, */*;q=0.8"}, http.StatusSeeOther},
		{"html tied with wildcard", []string{"text/html;q=0.8, */*;q=0.8"}, http.StatusUnauthorized},
		{"malformed", []string{"text/html;;broken"}, http.StatusUnauthorized},
		{"html with malformed bare range", []string{"text/html, broken"}, http.StatusUnauthorized},
		{"malformed bare range before html", []string{"broken, text/html"}, http.StatusUnauthorized},
		{"html with valid unrelated range", []string{"text/html, image/avif;q=1"}, http.StatusSeeOther},
		{"out of range q", []string{"text/html;q=1.5"}, http.StatusUnauthorized},
		{"non-numeric q", []string{"text/html;q=high"}, http.StatusUnauthorized},
		{"explicit json exclusion", []string{"text/html, application/json;q=0, */*;q=0"}, http.StatusSeeOther},
		{"json excluded wildcard weaker specificity", []string{"text/html;q=0.8, application/json;q=0, */*;q=1"}, http.StatusSeeOther},
		{"json exact beats application wildcard", []string{"text/html;q=0.8, application/json;q=0.5, application/*;q=1"}, http.StatusSeeOther},
		{"application wildcard excludes json", []string{"text/html;q=0.8, application/*;q=0, */*;q=1"}, http.StatusSeeOther},
		{"html q nan", []string{"text/html;q=NaN"}, http.StatusUnauthorized},
		{"json q nan", []string{"text/html, application/json;q=NaN"}, http.StatusUnauthorized},
		{"html q exponent", []string{"text/html;q=1e0"}, http.StatusUnauthorized},
		{"html q signed plus", []string{"text/html;q=+1"}, http.StatusUnauthorized},
		{"html q negative zero", []string{"text/html;q=-0"}, http.StatusUnauthorized},
		{"html q four decimals", []string{"text/html;q=0.0001"}, http.StatusUnauthorized},
		{"html q trailing zeros", []string{"text/html;q=1.0000"}, http.StatusUnauthorized},
		{"html q bare zero point", []string{"text/html;q=0."}, http.StatusUnauthorized},
		{"html q bare one point", []string{"text/html;q=1."}, http.StatusSeeOther},
		{"html q three decimals", []string{"text/html;q=0.123"}, http.StatusSeeOther},
		{"html q three zeros", []string{"text/html;q=1.000"}, http.StatusSeeOther},
		{"multiple header values", []string{"application/json", "text/html"}, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state=x&code=c", "")
			for _, value := range tc.accept {
				r.Header.Add("Accept", value)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if tc.want == http.StatusSeeOther {
				assertAdminBrowserRedirect(t, w, "authentication_rejected", "")
			} else {
				if w.Code != tc.want || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
					t.Fatalf("accept %v: %d %s", tc.accept, w.Code, w.Body.String())
				}
			}
		})
	}
}

// spec: marketplace-admin-auth/browser-and-machine-clients-receive-appropriate-failure-responses
func TestAdminOIDCBrowserUnavailableInitialization(t *testing.T) {
	issuer := newTestOIDCIssuer(t)
	_, handler := adminOIDCFixtureWithBrowserBase(t, issuer, "https://marketplace.test", func(admin *config.Administration) {
		admin.Authentication.OIDC.SessionSecretEnv = "MARKETPLACE_ADMIN_UNSET_SECRET"
	})
	for _, tc := range []struct {
		name     string
		method   string
		target   string
		accept   string
		wantCode int
		wantErr  string
	}{
		{"login html", http.MethodGet, "/api/v1/admin/auth/login", "text/html", http.StatusSeeOther, "admin_unavailable"},
		{"callback html", http.MethodGet, "/api/v1/admin/auth/callback?state=x&code=c", "text/html", http.StatusSeeOther, "admin_unavailable"},
		{"login json", http.MethodGet, "/api/v1/admin/auth/login", "application/json", http.StatusServiceUnavailable, "admin_unavailable"},
		{"data route stays json", http.MethodGet, "/api/v1/admin/session", "text/html", http.StatusServiceUnavailable, "admin_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := adminRequest(tc.method, tc.target, "")
			if tc.accept != "" {
				r.Header.Set("Accept", tc.accept)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if tc.wantCode == http.StatusSeeOther {
				location, err := url.Parse(w.Header().Get("Location"))
				if w.Code != http.StatusSeeOther || err != nil || location.Path != "/auth/error" || location.Query().Get("code") != tc.wantErr || location.Query().Has("next") {
					t.Fatalf("unavailable browser response = %d %q", w.Code, w.Header().Get("Location"))
				}
			} else {
				assertAdminJSONError(t, w, tc.wantCode, tc.wantErr, "Administration trust configuration is unavailable.")
			}
		})
	}
}

// spec: marketplace-admin-auth/browser-and-machine-clients-receive-appropriate-failure-responses
func TestAdminOIDCBrowserHeadRequestsStayJSON(t *testing.T) {
	issuer := newTestOIDCIssuer(t)
	_, handler := adminOIDCFixture(t, issuer, nil)

	t.Run("login discovery outage", func(t *testing.T) {
		issuer.discoveryCode = http.StatusServiceUnavailable
		defer func() { issuer.discoveryCode = 0 }()
		r := adminRequest(http.MethodHead, "/api/v1/admin/auth/login", "")
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assertAdminJSONError(t, w, http.StatusServiceUnavailable, "authentication_unavailable", "The identity provider is unavailable.")
	})
	t.Run("callback missing transaction", func(t *testing.T) {
		r := adminRequest(http.MethodHead, "/api/v1/admin/auth/callback?state=x&code=c", "")
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assertAdminJSONError(t, w, http.StatusUnauthorized, "authentication_rejected", "Sign-in could not be completed.")
	})
}

// spec: marketplace-admin-auth/browser-and-machine-clients-receive-appropriate-failure-responses
func TestAdminBrowserNonOIDCRoutesStayJSON(t *testing.T) {
	issuer := newTestOIDCIssuer(t)
	_, handler := adminOIDCFixture(t, issuer, nil)

	t.Run("session unauthenticated", func(t *testing.T) {
		r := adminRequest(http.MethodGet, "/api/v1/admin/session", "")
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assertAdminJSONError(t, w, http.StatusUnauthorized, "unauthenticated", "Sign in through the configured identity provider.")
	})
	t.Run("logout", func(t *testing.T) {
		r := adminRequest(http.MethodPost, "/api/v1/admin/auth/logout", "")
		r.Header.Set("Accept", "text/html")
		r.Header.Set("Origin", "https://marketplace.test")
		r.Header.Set("X-Marketplace-Admin", "1")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assertAdminJSONError(t, w, http.StatusOK, "signed_out", "")
	})
}
