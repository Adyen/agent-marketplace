package api

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/admin"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

// testAdminSessionSecret is the test-only session signing secret, resolved
// through MARKETPLACE_ADMIN_SESSION_SECRET like production configuration.
var testAdminSessionSecret = strings.Repeat("s", 48)

// HTTPS deployments consume the browser-enforced __Host- cookie names; the
// unprefixed logical names remain only on explicit loopback HTTP.
const (
	testSecureLoginCookie   = "__Host-marketplace_admin_login"
	testSecureSessionCookie = "__Host-marketplace_admin_session"
)

// testOIDCIssuer is an in-process OIDC issuer that signs real RS256 ID tokens.
type testOIDCIssuer struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	kid    string
	// claims applied to the next issued ID token; nonce must be set from the
	// decoded login transaction before the callback runs.
	claims     map[string]any
	omitClaims map[string]bool
	// verifierSeen records that token exchange presented PKCE material.
	verifierSeen          bool
	tokenCalls            int
	discoveryBody         []byte
	discoveryCode         int
	jwksBody              []byte
	jwksCode              int
	tokenBody             []byte
	tokenCode             int
	tokenAuthorization    string
	tokenClientID         string
	discoveryRedirectSize int
	discoveryWritten      chan int
}

func newTestOIDCIssuer(t *testing.T) *testOIDCIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	issuer := &testOIDCIssuer{key: key, kid: "test-key", claims: map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if issuer.discoveryCode != 0 {
			w.WriteHeader(issuer.discoveryCode)
		}
		if issuer.discoveryRedirectSize > 0 {
			chunk := []byte(strings.Repeat("x", 64<<10))
			written := 0
			for written < issuer.discoveryRedirectSize {
				remaining := issuer.discoveryRedirectSize - written
				if remaining < len(chunk) {
					chunk = chunk[:remaining]
				}
				n, err := w.Write(chunk)
				written += n
				if err != nil {
					break
				}
			}
			issuer.discoveryWritten <- written
			return
		}
		if issuer.discoveryBody != nil {
			_, _ = w.Write(issuer.discoveryBody)
			return
		}
		if issuer.discoveryCode != 0 {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": issuer.server.URL, "authorization_endpoint": issuer.server.URL + "/authorize",
			"token_endpoint": issuer.server.URL + "/token", "jwks_uri": issuer.server.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if issuer.jwksCode != 0 {
			w.WriteHeader(issuer.jwksCode)
		}
		if issuer.jwksBody != nil {
			_, _ = w.Write(issuer.jwksBody)
			return
		}
		if issuer.jwksCode != 0 {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "kid": issuer.kid, "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(issuer.key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(issuer.key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		issuer.tokenCalls++
		w.Header().Set("Content-Type", "application/json")
		if issuer.tokenCode != 0 {
			w.WriteHeader(issuer.tokenCode)
		}
		if issuer.tokenBody != nil {
			_, _ = w.Write(issuer.tokenBody)
			return
		}
		if issuer.tokenCode != 0 {
			_, _ = w.Write([]byte(`{"error":"temporarily_unavailable"}`))
			return
		}
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		issuer.tokenAuthorization = r.Header.Get("Authorization")
		issuer.tokenClientID = r.Form.Get("client_id")
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") == "" ||
			r.Form.Get("redirect_uri") == "" || len(r.Form.Get("code_verifier")) < 43 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		issuer.verifierSeen = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-access", "token_type": "Bearer", "expires_in": 300,
			"id_token": issuer.signIDToken(t, "marketplace-admin"),
		})
	})
	issuer.server = httptest.NewServer(mux)
	t.Cleanup(issuer.server.Close)
	return issuer
}

func (i *testOIDCIssuer) discoveryDocument(t *testing.T) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"issuer": i.server.URL, "authorization_endpoint": i.server.URL + "/authorize",
		"token_endpoint": i.server.URL + "/token", "jwks_uri": i.server.URL + "/jwks",
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func (i *testOIDCIssuer) jwksDocument(t *testing.T) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "kid": i.kid, "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(i.key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(i.key.E)).Bytes()),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func (i *testOIDCIssuer) signIDToken(t *testing.T, audience string) string {
	t.Helper()
	now := time.Now()
	claims := map[string]any{
		"iss": i.server.URL, "aud": audience, "sub": "okta-subject-1",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": "",
	}
	for key, value := range i.claims {
		claims[key] = value
	}
	for key := range i.omitClaims {
		delete(claims, key)
	}
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": i.kid, "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, i.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// rotateSigningKey replaces the signing key and key ID so subsequently issued
// tokens reference a key ID the verifier has not seen in its cached JWKS.
func (i *testOIDCIssuer) rotateSigningKey(t *testing.T) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	i.key = key
	i.kid = "rotated-key"
}

// sealAdminPayload signs a cookie payload exactly like the authenticator so
// tests can construct expired or malformed transactions and sessions.
func sealAdminPayload(t *testing.T, payload any) string {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(testAdminSessionSecret))
	_, _ = mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func adminOIDCFixture(t *testing.T, issuer *testOIDCIssuer, mutate func(*config.Administration)) (*Server, http.Handler) {
	t.Helper()
	t.Setenv("MARKETPLACE_ADMIN_SESSION_SECRET", testAdminSessionSecret)
	meta := sqlite.New(":memory:")
	if err := meta.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	cfg := config.Config{
		Instance:   config.Instance{ID: "admin-test", Name: "Test", CanonicalURL: "https://marketplace.test"},
		Management: config.Management{Mode: "database"},
		Policy:     config.Policy{LicenseAllowlist: []string{"MIT"}, LicenseUndeclared: "reject"},
		Admin: config.Administration{
			Enabled: true, Origin: "https://marketplace.test", AdminGroups: []string{"marketplace-admins"},
			AllowInsecureLocalhost: true,
			Authentication: config.AdminAuthentication{Mode: config.AdminAuthOIDC, OIDC: config.AdminOIDC{
				Issuer: issuer.server.URL, ClientID: "marketplace-admin", SessionSecretEnv: "MARKETPLACE_ADMIN_SESSION_SECRET",
			}},
		},
	}
	if mutate != nil {
		mutate(&cfg.Admin)
	}
	if err := cfg.Admin.Validate(); err != nil {
		t.Fatalf("fixture config invalid: %v", err)
	}
	svc := admin.Service{Metadata: meta, Deployment: cfg.Management}
	if _, err := svc.Adopt(context.Background(), cfg, admin.AdoptionRequest{Actor: store.ManagedActor{Issuer: "setup", Subject: "operator"}, Reason: "Adopt test", RequestID: "adopt", IdempotencyKey: "adopt"}); err != nil {
		t.Fatal(err)
	}
	// Exercise the public listener seam end to end: admin routes are mounted
	// on the same mux as the anonymous public API.
	s := NewServer(cfg, meta, nil)
	return s, s.PublicHandler()
}

func adminRequest(method, target string, cookie string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.RemoteAddr = "192.0.2.10:4567"
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	return r
}

func startOIDCLogin(t *testing.T, handler http.Handler) (*httptest.ResponseRecorder, adminLoginTransaction) {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/auth/login", ""))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	return w, decodeLoginTransaction(t, cookieValue(t, w, testSecureLoginCookie))
}

func completeOIDCCallback(t *testing.T, handler http.Handler, login *httptest.ResponseRecorder, transaction adminLoginTransaction) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state="+transaction.State+"&code=c", testSecureLoginCookie+"="+cookieValue(t, login, testSecureLoginCookie)))
	return w
}

func adminErrorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode admin error: %v: %s", err, w.Body.String())
	}
	return body.Code
}

func cookieValue(t *testing.T, w *httptest.ResponseRecorder, name string) string {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	t.Fatalf("cookie %s not set; headers %v", name, w.Header())
	return ""
}

func decodeLoginTransaction(t *testing.T, value string) adminLoginTransaction {
	t.Helper()
	body64, _, ok := strings.Cut(value, ".")
	if !ok {
		t.Fatal("transaction cookie is not signed")
	}
	payload, err := base64.RawURLEncoding.DecodeString(body64)
	if err != nil {
		t.Fatal(err)
	}
	var transaction adminLoginTransaction
	if err := json.Unmarshal(payload, &transaction); err != nil {
		t.Fatal(err)
	}
	return transaction
}

func TestAdminOIDCLoginRedirect(t *testing.T) {
	issuer := newTestOIDCIssuer(t)
	_, handler := adminOIDCFixture(t, issuer, nil)
	for _, tc := range []struct {
		next string
		want int
	}{{"", 303}, {"/marketplace/admin", 303}, {"/marketplace/admin/registries/team/configuration", 303},
		{"https://evil.test", 400}, {"//evil.test", 400}, {"/other", 400}, {"/marketplace/adminx", 400},
		{"/marketplace/admin/../secret", 400}, {"/marketplace/admin/%2e%2e/x", 400}, {"/marketplace/admin\\evil", 400},
		{"https://marketplace.test/marketplace/admin", 400}} {
		target := "/api/v1/admin/auth/login"
		if tc.next != "" {
			target += "?next=" + tc.next
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, adminRequest(http.MethodGet, target, ""))
		if w.Code != tc.want {
			t.Fatalf("next=%q: %d body=%s", tc.next, w.Code, w.Body.String())
		}
		if tc.want == 303 {
			location := w.Header().Get("Location")
			if !strings.HasPrefix(location, issuer.server.URL+"/authorize?") {
				t.Fatalf("next=%q location=%q", tc.next, location)
			}
			for _, param := range []string{"client_id=marketplace-admin", "response_type=code", "code_challenge_method=S256", "scope=openid+profile+email+groups", "state=", "nonce=", "code_challenge=", "redirect_uri=https%3A%2F%2Fmarketplace.test%2Fapi%2Fv1%2Fadmin%2Fauth%2Fcallback"} {
				if !strings.Contains(location, param) {
					t.Fatalf("next=%q location missing %q: %s", tc.next, param, location)
				}
			}
			for _, c := range w.Result().Cookies() {
				if c.Name == testSecureLoginCookie && (!c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "") {
					t.Fatalf("transaction cookie attributes: %+v", c)
				}
			}
			txn := decodeLoginTransaction(t, cookieValue(t, w, testSecureLoginCookie))
			if txn.State == "" || txn.Nonce == "" || txn.Verifier == "" || txn.Version != 1 ||
				txn.IssuedAt <= 0 || txn.Expiry-txn.IssuedAt != int64(adminLoginTTL.Seconds()) {
				t.Fatalf("transaction incomplete: %+v", txn)
			}
		}
	}
}

func TestAdminOIDCResponseBounds(t *testing.T) {
	t.Run("discovery", func(t *testing.T) {
		issuer := newTestOIDCIssuer(t)
		issuer.discoveryBody = append(issuer.discoveryDocument(t), []byte(strings.Repeat(" ", (1<<20)+1))...)
		_, handler := adminOIDCFixture(t, issuer, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/auth/login", ""))
		if w.Code != http.StatusServiceUnavailable || adminErrorCode(t, w) != "authentication_unavailable" {
			t.Fatalf("oversized discovery response: %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("JWKS", func(t *testing.T) {
		issuer := newTestOIDCIssuer(t)
		_, handler := adminOIDCFixture(t, issuer, nil)
		login, transaction := startOIDCLogin(t, handler)
		issuer.claims = map[string]any{"nonce": transaction.Nonce, "groups": []string{"marketplace-admins"}}
		issuer.jwksBody = append(issuer.jwksDocument(t), []byte(strings.Repeat(" ", (1<<20)+1))...)
		w := completeOIDCCallback(t, handler, login, transaction)
		if w.Code != http.StatusServiceUnavailable || adminErrorCode(t, w) != "authentication_unavailable" {
			t.Fatalf("oversized JWKS response: %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("terminal redirect", func(t *testing.T) {
		issuer := newTestOIDCIssuer(t)
		issuer.discoveryCode = http.StatusFound
		issuer.discoveryRedirectSize = 32 << 20
		issuer.discoveryWritten = make(chan int, 1)
		_, handler := adminOIDCFixture(t, issuer, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/auth/login", ""))
		if w.Code != http.StatusServiceUnavailable || adminErrorCode(t, w) != "authentication_unavailable" {
			t.Fatalf("oversized discovery redirect: %d %s", w.Code, w.Body.String())
		}
		select {
		case written := <-issuer.discoveryWritten:
			if written >= issuer.discoveryRedirectSize {
				t.Fatalf("read complete oversized redirect body: %d bytes", written)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("provider did not observe the bounded response close")
		}
	})
}

func TestAdminOIDCProviderFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tokenCode int
		jwksCode  int
		jwksBody  []byte
		close     bool
		want      int
		wantCode  string
	}{
		{name: "token network failure", close: true, want: http.StatusServiceUnavailable, wantCode: "authentication_unavailable"},
		{name: "token unavailable", tokenCode: http.StatusServiceUnavailable, want: http.StatusServiceUnavailable, wantCode: "authentication_unavailable"},
		{name: "token rate limited", tokenCode: http.StatusTooManyRequests, want: http.StatusServiceUnavailable, wantCode: "authentication_unavailable"},
		{name: "invalid grant", tokenCode: http.StatusBadRequest, want: http.StatusUnauthorized, wantCode: "authentication_rejected"},
		{name: "JWKS unavailable", jwksCode: http.StatusServiceUnavailable, want: http.StatusServiceUnavailable, wantCode: "authentication_unavailable"},
		{name: "JWKS malformed", jwksBody: []byte(`{"keys":`), want: http.StatusServiceUnavailable, wantCode: "authentication_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issuer := newTestOIDCIssuer(t)
			_, handler := adminOIDCFixture(t, issuer, nil)
			login, transaction := startOIDCLogin(t, handler)
			issuer.claims = map[string]any{"nonce": transaction.Nonce, "groups": []string{"marketplace-admins"}}
			issuer.tokenCode = tc.tokenCode
			issuer.jwksCode = tc.jwksCode
			issuer.jwksBody = tc.jwksBody
			if tc.close {
				issuer.server.Close()
			}
			w := completeOIDCCallback(t, handler, login, transaction)
			if w.Code != tc.want || adminErrorCode(t, w) != tc.wantCode {
				t.Fatalf("callback: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAdminOIDCCanonicalOriginsAccepted(t *testing.T) {
	t.Setenv("MARKETPLACE_ADMIN_SESSION_SECRET", testAdminSessionSecret)
	for _, origin := range []string{"https://marketplace.test:8443", "https://xn--bcher-kva.test", "https://127.0.0.1", "https://[::1]:8443"} {
		t.Run(origin, func(t *testing.T) {
			admin := config.Administration{
				Enabled: true, Origin: origin, AdminGroups: []string{"marketplace-admins"},
				Authentication: config.AdminAuthentication{Mode: config.AdminAuthOIDC, OIDC: config.AdminOIDC{
					Issuer: "https://issuer.test", ClientID: "marketplace-admin", SessionSecretEnv: "MARKETPLACE_ADMIN_SESSION_SECRET",
				}},
			}
			authenticator, err := newAdminOIDCAuthenticator(admin)
			if err != nil {
				t.Fatalf("canonical origin %q rejected: %v", origin, err)
			}
			if authenticator.redirectURL != origin+adminAuthBase+"/callback" {
				t.Fatalf("redirect URL changed for %q: %q", origin, authenticator.redirectURL)
			}
		})
	}
}

func TestAdminOIDCNonCanonicalOriginRejected(t *testing.T) {
	for _, origin := range []string{"HTTPS://marketplace.test", "https://Marketplace.test", "https://marketplace.test:443", "https://marketplace.test.", "https://marketplace.test:08080", "https://bücher.test", "https://2130706433", "https://0x7f000001", "https://0177.0.0.1", "https://0x7f.0.0.1", "https://127.0.0.01",
		"https://[::ffff:192.0.2.1]", "https://[::ffff:c000:0201]", "https://[0:0:0:0:0:ffff:c000:201]", "https://[::ffff:127.0.0.1]"} {
		t.Run(origin, func(t *testing.T) {
			t.Setenv("MARKETPLACE_ADMIN_SESSION_SECRET", testAdminSessionSecret)
			admin := config.Administration{
				Enabled: true, Origin: origin, AdminGroups: []string{"marketplace-admins"},
				Authentication: config.AdminAuthentication{Mode: config.AdminAuthOIDC, OIDC: config.AdminOIDC{
					Issuer: "https://issuer.test", ClientID: "marketplace-admin", SessionSecretEnv: "MARKETPLACE_ADMIN_SESSION_SECRET",
				}},
			}
			if admin.Validate() == nil {
				t.Fatalf("non-canonical origin %q accepted", origin)
			}
			if _, err := newAdminOIDCAuthenticator(admin); err == nil {
				t.Fatalf("authenticator accepted non-canonical origin %q", origin)
			}
		})
	}
}

func TestAdminOIDCCallbackAndSession(t *testing.T) {
	issuer := newTestOIDCIssuer(t)
	s, handler := adminOIDCFixture(t, issuer, nil)
	_ = s
	login := func(next string) (*httptest.ResponseRecorder, adminLoginTransaction) {
		target := "/api/v1/admin/auth/login"
		if next != "" {
			target += "?next=" + next
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, adminRequest(http.MethodGet, target, ""))
		if w.Code != 303 {
			t.Fatalf("login: %d %s", w.Code, w.Body.String())
		}
		return w, decodeLoginTransaction(t, cookieValue(t, w, testSecureLoginCookie))
	}
	callback := func(txn adminLoginTransaction, state, code, cookie string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state="+state+"&code="+code, testSecureLoginCookie+"="+cookie))
		return w
	}
	loginCookieOf := func(w *httptest.ResponseRecorder) string {
		return cookieValue(t, w, testSecureLoginCookie)
	}

	t.Run("group authorized session", func(t *testing.T) {
		w, txn := login("/marketplace/admin/registries/team")
		issuer.claims = map[string]any{"nonce": txn.Nonce, "groups": []string{"other", "marketplace-admins"}, "name": "Operator One"}
		resp := callback(txn, txn.State, "code-1", loginCookieOf(w))
		if resp.Code != 303 || resp.Header().Get("Location") != "/marketplace/admin/registries/team" {
			t.Fatalf("callback: %d %q %s", resp.Code, resp.Header().Get("Location"), resp.Body.String())
		}
		if !issuer.verifierSeen {
			t.Fatal("token exchange did not present a PKCE verifier")
		}
		if issuer.tokenCalls != 1 || issuer.tokenAuthorization != "" || issuer.tokenClientID != "marketplace-admin" {
			t.Fatalf("public client exchange: calls=%d authorization=%q client_id=%q", issuer.tokenCalls, issuer.tokenAuthorization, issuer.tokenClientID)
		}
		sessionRaw := cookieValue(t, resp, testSecureSessionCookie)
		for _, cookie := range resp.Result().Cookies() {
			if cookie.Name == testSecureSessionCookie {
				if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
					t.Fatalf("session cookie attributes: %+v", cookie)
				}
			}
		}
		r := adminRequest(http.MethodGet, "/api/v1/admin/session", testSecureSessionCookie+"="+sessionRaw)
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("session: %d %s", w.Code, w.Body.String())
		}
		var session struct {
			Actor   store.ManagedActor `json:"actor"`
			Display string             `json:"display"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
			t.Fatal(err)
		}
		if session.Actor.Issuer != issuer.server.URL || session.Actor.Subject != "okta-subject-1" || session.Display != "Operator One" {
			t.Fatalf("actor: %+v", session)
		}
	})

	t.Run("subject authorized via admin_users", func(t *testing.T) {
		issuer2 := newTestOIDCIssuer(t)
		_, handler2 := adminOIDCFixture(t, issuer2, func(a *config.Administration) {
			a.AdminGroups = nil
			a.AdminUsers = []string{"okta-subject-1"}
			a.Authentication.OIDC.Issuer = issuer2.server.URL
		})
		w := httptest.NewRecorder()
		handler2.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/auth/login", ""))
		txn := decodeLoginTransaction(t, loginCookieOf(w))
		issuer2.claims = map[string]any{"nonce": txn.Nonce}
		resp := httptest.NewRecorder()
		handler2.ServeHTTP(resp, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state="+txn.State+"&code=c", testSecureLoginCookie+"="+loginCookieOf(w)))
		if resp.Code != 303 {
			t.Fatalf("callback: %d %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("unauthorized identity is forbidden", func(t *testing.T) {
		w, txn := login("")
		issuer.claims = map[string]any{"nonce": txn.Nonce, "groups": []string{"catalog-users"}, "sub": "okta-other"}
		resp := callback(txn, txn.State, "code-2", loginCookieOf(w))
		if resp.Code != 403 {
			t.Fatalf("callback: %d %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("bad state rejected", func(t *testing.T) {
		w, txn := login("")
		issuer.claims = map[string]any{"nonce": txn.Nonce, "groups": []string{"marketplace-admins"}}
		resp := callback(txn, "wrong-state", "code-3", loginCookieOf(w))
		if resp.Code != 401 {
			t.Fatalf("callback: %d %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("provider error rejected", func(t *testing.T) {
		w, _ := login("")
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?error=access_denied&state=x", testSecureLoginCookie+"="+loginCookieOf(w)))
		if resp.Code != 401 {
			t.Fatalf("callback: %d %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("missing transaction rejected", func(t *testing.T) {
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state=x&code=y", ""))
		if resp.Code != 401 {
			t.Fatalf("callback: %d", resp.Code)
		}
	})

	t.Run("wrong nonce rejected", func(t *testing.T) {
		w, txn := login("")
		issuer.claims = map[string]any{"nonce": "different", "groups": []string{"marketplace-admins"}}
		resp := callback(txn, txn.State, "code-4", loginCookieOf(w))
		if resp.Code != 401 {
			t.Fatalf("callback: %d %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("tampered session rejected", func(t *testing.T) {
		w, txn := login("")
		issuer.claims = map[string]any{"nonce": txn.Nonce, "groups": []string{"marketplace-admins"}}
		resp := callback(txn, txn.State, "code-5", loginCookieOf(w))
		raw := cookieValue(t, resp, testSecureSessionCookie)
		tampered := raw[:len(raw)-4] + "AAAA"
		r := adminRequest(http.MethodGet, "/api/v1/admin/session", testSecureSessionCookie+"="+tampered)
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("tampered session: %d", w.Code)
		}
	})

	t.Run("missing session unauthenticated", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/session", ""))
		var response struct {
			Fields map[string]string `json:"fields"`
		}
		if w.Code != 401 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Fields["authentication_mode"] != "oidc" {
			t.Fatalf("missing session: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("logout is a protected idempotent post", func(t *testing.T) {
		logout := func(mutate func(*http.Request)) *httptest.ResponseRecorder {
			r := adminRequest(http.MethodPost, "/api/v1/admin/auth/logout", "")
			r.Header.Set("Origin", "https://marketplace.test")
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Marketplace-Admin", "1")
			if mutate != nil {
				mutate(r)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			return w
		}
		if w := logout(nil); w.Code != 200 {
			t.Fatalf("logout: %d %s", w.Code, w.Body.String())
		}
		if w := logout(nil); w.Code != 200 {
			t.Fatalf("logout repeat: %d", w.Code)
		}
		if w := logout(func(r *http.Request) { r.Header.Del("X-Marketplace-Admin") }); w.Code != 403 {
			t.Fatalf("logout without header: %d", w.Code)
		}
		if w := logout(func(r *http.Request) { r.Header.Set("Origin", "https://other.test") }); w.Code != 403 {
			t.Fatalf("logout foreign origin: %d", w.Code)
		}
		if w := logout(func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }); w.Code != 415 {
			t.Fatalf("logout non-json: %d", w.Code)
		}
	})

	t.Run("public routes stay anonymous", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/auth/unknown", ""))
		if w.Code != 401 && w.Code != 404 {
			t.Fatalf("unexpected auth route status: %d", w.Code)
		}
	})

	t.Run("transaction window enforced", func(t *testing.T) {
		now := time.Now().Unix()
		base := adminLoginTransaction{Version: 1, State: adminRandom(), Nonce: adminRandom(), Verifier: adminRandom(), Next: "/marketplace/admin"}
		for _, tc := range []struct {
			name string
			iat  int64
			exp  int64
		}{
			{"expired", now - 1200, now - 600},
			{"future issued-at", now + 60, now + 660},
			{"window beyond ttl", now - 60, now + 3600},
			{"missing issued-at", 0, now + 600},
		} {
			txn := base
			txn.IssuedAt, txn.Expiry = tc.iat, tc.exp
			cookie := sealAdminPayload(t, txn)
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state="+txn.State+"&code=x", testSecureLoginCookie+"="+cookie))
			if resp.Code != 401 {
				t.Fatalf("%s: %d %s", tc.name, resp.Code, resp.Body.String())
			}
		}
	})

	t.Run("tampered transaction rejected", func(t *testing.T) {
		now := time.Now().Unix()
		txn := adminLoginTransaction{Version: 1, State: adminRandom(), Nonce: adminRandom(), Verifier: adminRandom(), Next: "/marketplace/admin", IssuedAt: now, Expiry: now + int64(adminLoginTTL.Seconds())}
		raw := sealAdminPayload(t, txn)
		tampered := raw[:len(raw)-4] + "AAAA"
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state="+txn.State+"&code=x", testSecureLoginCookie+"="+tampered))
		if resp.Code != 401 {
			t.Fatalf("tampered transaction: %d %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("expired session rejected", func(t *testing.T) {
		now := time.Now().Unix()
		session := sealAdminPayload(t, adminSessionPayload{
			Version: 1, Issuer: issuer.server.URL, Subject: "okta-subject-1",
			Groups: []string{"marketplace-admins"}, IssuedAt: now - 7200, Expiry: now - 60,
		})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/session", testSecureSessionCookie+"="+session))
		if w.Code != 401 {
			t.Fatalf("expired session: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("unknown key id refreshes JWKS", func(t *testing.T) {
		// Prime the verifier's cached key set with the original signing key.
		w, txn := login("")
		issuer.claims = map[string]any{"nonce": txn.Nonce, "groups": []string{"marketplace-admins"}}
		resp := callback(txn, txn.State, "code-prime", loginCookieOf(w))
		if resp.Code != 303 {
			t.Fatalf("priming callback: %d %s", resp.Code, resp.Body.String())
		}
		// Rotating the key leaves the cached JWKS without the new key ID; the
		// verifier must perform its single bounded remote refetch.
		issuer.rotateSigningKey(t)
		w, txn = login("")
		issuer.claims = map[string]any{"nonce": txn.Nonce, "groups": []string{"marketplace-admins"}}
		resp = callback(txn, txn.State, "code-rotated", loginCookieOf(w))
		if resp.Code != 303 {
			t.Fatalf("rotated key callback: %d %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("oidc session authorizes admin mutation", func(t *testing.T) {
		w, txn := login("")
		issuer.claims = map[string]any{"nonce": txn.Nonce, "groups": []string{"marketplace-admins"}}
		resp := callback(txn, txn.State, "code-mutation", loginCookieOf(w))
		if resp.Code != 303 {
			t.Fatalf("callback: %d %s", resp.Code, resp.Body.String())
		}
		session := cookieValue(t, resp, testSecureSessionCookie)
		body := `{"action":"set_policy","license_policy":{"allowlist":["MIT","Apache-2.0"],"undeclared":"reject"},"reason":"OIDC change"}`
		r := adminRequest(http.MethodPost, "/api/v1/admin/changes", testSecureSessionCookie+"="+session)
		r.Header.Set("Origin", "https://marketplace.test")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Marketplace-Admin", "1")
		r.Header.Set("If-Match", `"1"`)
		r.Header.Set("Idempotency-Key", "oidc-change-1")
		r.Body = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)).Body
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 202 {
			t.Fatalf("apply: %d %s", w.Code, w.Body.String())
		}
		var accepted store.ManagedChange
		if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
			t.Fatal(err)
		}
		if accepted.Actor.Issuer != issuer.server.URL || accepted.Actor.Subject != "okta-subject-1" {
			t.Fatalf("actor did not flow to change: %+v", accepted.Actor)
		}
		// The audit history records the same immutable issuer and subject.
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/activity", testSecureSessionCookie+"="+session))
		if w.Code != 200 {
			t.Fatalf("activity: %d %s", w.Code, w.Body.String())
		}
		var activity struct {
			Changes []store.ManagedChange `json:"changes"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &activity); err != nil {
			t.Fatal(err)
		}
		if len(activity.Changes) == 0 || activity.Changes[0].Actor.Issuer != issuer.server.URL || activity.Changes[0].Actor.Subject != "okta-subject-1" {
			t.Fatalf("audit actor: %+v", activity.Changes)
		}
	})
}

func TestAdminOIDCCookieBrowserDeliveryBound(t *testing.T) {
	issuer := newTestOIDCIssuer(t)
	_, handler := adminOIDCFixture(t, issuer, nil)
	login := func(next string) (*httptest.ResponseRecorder, adminLoginTransaction) {
		target := "/api/v1/admin/auth/login"
		if next != "" {
			target += "?next=" + next
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, adminRequest(http.MethodGet, target, ""))
		if w.Code != 303 {
			t.Fatalf("login: %d %s", w.Code, w.Body.String())
		}
		for _, line := range w.Header().Values("Set-Cookie") {
			if len(line) > 4096 {
				t.Fatalf("login Set-Cookie exceeds browser limit: %d bytes", len(line))
			}
		}
		return w, decodeLoginTransaction(t, cookieValue(t, w, testSecureLoginCookie))
	}
	callback := func(txn adminLoginTransaction, cookie string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state="+txn.State+"&code=c", testSecureLoginCookie+"="+cookie))
		return w
	}
	sessionSet := func(w *httptest.ResponseRecorder) bool {
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == testSecureSessionCookie && cookie.Value != "" {
				return true
			}
		}
		return false
	}

	t.Run("worst-case login transaction stays browser deliverable", func(t *testing.T) {
		w, txn := login("/marketplace/admin/" + strings.Repeat("a", 2048-len("/marketplace/admin/")))
		issuer.claims = map[string]any{"nonce": txn.Nonce, "groups": []string{"marketplace-admins"}, "name": "Operator One"}
		resp := callback(txn, cookieValue(t, w, testSecureLoginCookie))
		if resp.Code != 303 {
			t.Fatalf("worst-case callback: %d %s", resp.Code, resp.Body.String())
		}
		for _, line := range resp.Header().Values("Set-Cookie") {
			if len(line) > 4096 {
				t.Fatalf("callback Set-Cookie exceeds browser limit: %d bytes", len(line))
			}
		}
		if !sessionSet(resp) {
			t.Fatal("session cookie not set")
		}
	})

	t.Run("oversized verified groups rejected without session", func(t *testing.T) {
		w, txn := login("")
		groups := []string{"marketplace-admins"}
		for i := 0; i < 20; i++ {
			groups = append(groups, "group-"+strings.Repeat("x", 200))
		}
		issuer.claims = map[string]any{"nonce": txn.Nonce, "groups": groups}
		resp := callback(txn, cookieValue(t, w, testSecureLoginCookie))
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("oversized session: %d %s", resp.Code, resp.Body.String())
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil || body.Code != "authentication_rejected" {
			t.Fatalf("oversized session code: %s", resp.Body.String())
		}
		if sessionSet(resp) {
			t.Fatal("oversized identity must not receive a session cookie")
		}
	})
}

func TestAdminOIDCAuthorizedParty(t *testing.T) {
	issuer := newTestOIDCIssuer(t)
	_, handler := adminOIDCFixture(t, issuer, nil)
	for _, tc := range []struct {
		name   string
		claims map[string]any
		want   int
	}{
		{"single audience without azp", map[string]any{}, http.StatusSeeOther},
		{"multiple audiences without azp", map[string]any{"aud": []string{"marketplace-admin", "other-client"}}, http.StatusUnauthorized},
		{"multiple audiences mismatched azp", map[string]any{"aud": []string{"marketplace-admin", "other-client"}, "azp": "other-client"}, http.StatusUnauthorized},
		{"multiple audiences matching azp", map[string]any{"aud": []string{"marketplace-admin", "other-client"}, "azp": "marketplace-admin"}, http.StatusSeeOther},
		{"single audience mismatched azp", map[string]any{"azp": "other-client"}, http.StatusUnauthorized},
		{"wrong-typed azp", map[string]any{"azp": []string{"marketplace-admin"}}, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/auth/login", ""))
			if w.Code != 303 {
				t.Fatalf("login: %d %s", w.Code, w.Body.String())
			}
			cookie := cookieValue(t, w, testSecureLoginCookie)
			txn := decodeLoginTransaction(t, cookie)
			issuer.claims = map[string]any{"nonce": txn.Nonce, "groups": []string{"marketplace-admins"}}
			for key, value := range tc.claims {
				issuer.claims[key] = value
			}
			resp := httptest.NewRecorder()
			handler.ServeHTTP(resp, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state="+txn.State+"&code=c", testSecureLoginCookie+"="+cookie))
			if resp.Code != tc.want {
				t.Fatalf("callback: %d %s", resp.Code, resp.Body.String())
			}
		})
	}
}

func TestAdminOIDCIssuedAtValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testOIDCIssuer)
		want   int
	}{
		{name: "current", want: http.StatusSeeOther},
		{name: "missing", mutate: func(issuer *testOIDCIssuer) { issuer.omitClaims = map[string]bool{"iat": true} }, want: http.StatusUnauthorized},
		{name: "null", mutate: func(issuer *testOIDCIssuer) { issuer.claims["iat"] = nil }, want: http.StatusUnauthorized},
		{name: "arbitrarily old", mutate: func(issuer *testOIDCIssuer) { issuer.claims["iat"] = time.Now().Add(-time.Hour).Unix() }, want: http.StatusUnauthorized},
		{name: "beyond clock skew", mutate: func(issuer *testOIDCIssuer) {
			issuer.claims["iat"] = time.Now().Add(adminClockSkew + time.Minute).Unix()
		}, want: http.StatusUnauthorized},
		{name: "expiry within clock skew", mutate: func(issuer *testOIDCIssuer) {
			issuer.claims["iat"] = time.Now().Add(-2 * time.Minute).Unix()
			issuer.claims["exp"] = time.Now().Add(-time.Minute).Unix()
		}, want: http.StatusSeeOther},
		{name: "expiry beyond clock skew", mutate: func(issuer *testOIDCIssuer) {
			issuer.claims["iat"] = time.Now().Add(-10 * time.Minute).Unix()
			issuer.claims["exp"] = time.Now().Add(-adminClockSkew - time.Minute).Unix()
		}, want: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issuer := newTestOIDCIssuer(t)
			_, handler := adminOIDCFixture(t, issuer, nil)
			login, transaction := startOIDCLogin(t, handler)
			issuer.claims = map[string]any{"nonce": transaction.Nonce, "groups": []string{"marketplace-admins"}}
			if tc.mutate != nil {
				tc.mutate(issuer)
			}
			w := completeOIDCCallback(t, handler, login, transaction)
			if w.Code != tc.want {
				t.Fatalf("callback: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAdminOIDCNonDefaultGroupsClaim(t *testing.T) {
	issuer := newTestOIDCIssuer(t)
	_, handler := adminOIDCFixture(t, issuer, func(a *config.Administration) {
		a.Authentication.OIDC.GroupsClaim = "roles"
	})
	login := func() (*httptest.ResponseRecorder, adminLoginTransaction) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/auth/login", ""))
		if w.Code != 303 {
			t.Fatalf("login: %d %s", w.Code, w.Body.String())
		}
		return w, decodeLoginTransaction(t, cookieValue(t, w, testSecureLoginCookie))
	}
	callback := func(txn adminLoginTransaction, cookie string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state="+txn.State+"&code=c", testSecureLoginCookie+"="+cookie))
		return w
	}
	w, txn := login()
	issuer.claims = map[string]any{"nonce": txn.Nonce, "roles": []string{"marketplace-admins"}, "groups": []string{"catalog-users"}}
	if resp := callback(txn, cookieValue(t, w, testSecureLoginCookie)); resp.Code != 303 {
		t.Fatalf("roles claim: %d %s", resp.Code, resp.Body.String())
	}
	w, txn = login()
	issuer.claims = map[string]any{"nonce": txn.Nonce, "groups": []string{"marketplace-admins"}, "roles": []string{"catalog-users"}}
	if resp := callback(txn, cookieValue(t, w, testSecureLoginCookie)); resp.Code != 403 {
		t.Fatalf("default groups claim must not authorize: %d %s", resp.Code, resp.Body.String())
	}
}

func TestAdminOIDCFailsClosedOnMisconfiguration(t *testing.T) {
	issuer := newTestOIDCIssuer(t)
	t.Setenv("MARKETPLACE_ADMIN_SESSION_SECRET", testAdminSessionSecret)
	meta := sqlite.New(":memory:")
	if err := meta.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	cfg := config.Config{Instance: config.Instance{ID: "admin-test", Name: "Test", CanonicalURL: "https://marketplace.test"},
		Admin: config.Administration{Enabled: true, Origin: "https://marketplace.test", AdminGroups: []string{"admins"},
			Authentication: config.AdminAuthentication{Mode: "oidc", OIDC: config.AdminOIDC{Issuer: issuer.server.URL, ClientID: "c"}}}}
	handler := NewServer(cfg, meta, nil).PublicHandler()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/session", ""))
	if w.Code != 503 {
		t.Fatalf("invalid auth config must fail closed: %d", w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, adminRequest(http.MethodGet, "/healthz", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("invalid admin auth config affected public health route: %d %s", w.Code, w.Body.String())
	}
}

func adminCookieByName(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func requireAdminCookie(t *testing.T, w *httptest.ResponseRecorder, name, path string, secure bool) *http.Cookie {
	t.Helper()
	cookie := adminCookieByName(w, name)
	if cookie == nil {
		t.Fatalf("cookie %s not set; headers %v", name, w.Header())
	}
	if cookie.Path != path || cookie.Secure != secure || cookie.Domain != "" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie %s attributes: %+v", name, cookie)
	}
	return cookie
}

func TestAdminOIDCSecureCookieBoundary(t *testing.T) {
	issuer := newTestOIDCIssuer(t)
	_, handler := adminOIDCFixture(t, issuer, nil)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/auth/login", ""))
	if w.Code != 303 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	login := requireAdminCookie(t, w, testSecureLoginCookie, "/", true)
	if adminCookieByName(w, adminLoginCookie) != nil {
		t.Fatal("HTTPS login set the unprefixed transaction cookie")
	}
	txn := decodeLoginTransaction(t, login.Value)
	issuer.claims = map[string]any{"nonce": txn.Nonce, "groups": []string{"marketplace-admins"}}

	// A sibling unprefixed same-logical-name cookie must not satisfy the
	// callback lookup; the browser-enforced name cannot collide with it.
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state="+txn.State+"&code=c", adminLoginCookie+"="+login.Value))
	if resp.Code != 401 {
		t.Fatalf("unprefixed transaction cookie accepted: %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state="+txn.State+"&code=c", testSecureLoginCookie+"="+login.Value))
	if resp.Code != 303 {
		t.Fatalf("callback: %d %s", resp.Code, resp.Body.String())
	}
	if expired := requireAdminCookie(t, resp, testSecureLoginCookie, "/", true); expired.MaxAge >= 0 {
		t.Fatalf("transaction cookie not expired under secure name: %+v", expired)
	}
	session := requireAdminCookie(t, resp, testSecureSessionCookie, "/", true)
	if session.Value == "" || session.MaxAge <= 0 {
		t.Fatalf("session cookie: %+v", session)
	}

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/session", testSecureSessionCookie+"="+session.Value))
	if w.Code != 200 {
		t.Fatalf("secure session: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/session", adminSessionCookie+"="+session.Value))
	if w.Code != 401 {
		t.Fatalf("unprefixed session cookie accepted: %d", w.Code)
	}

	r := adminRequest(http.MethodPost, "/api/v1/admin/auth/logout", testSecureSessionCookie+"="+session.Value)
	r.Header.Set("Origin", "https://marketplace.test")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Marketplace-Admin", "1")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("logout: %d %s", w.Code, w.Body.String())
	}
	for _, name := range []string{testSecureSessionCookie, testSecureLoginCookie} {
		if expired := requireAdminCookie(t, w, name, "/", true); expired.MaxAge >= 0 {
			t.Fatalf("logout did not expire %s: %+v", name, expired)
		}
	}
}

func TestAdminOIDCLoopbackHTTPCookies(t *testing.T) {
	issuer := newTestOIDCIssuer(t)
	_, handler := adminOIDCFixture(t, issuer, func(a *config.Administration) {
		a.Origin = "http://localhost:8080"
	})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/auth/login", ""))
	if w.Code != 303 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	login := requireAdminCookie(t, w, adminLoginCookie, adminAuthBase, false)
	if adminCookieByName(w, testSecureLoginCookie) != nil {
		t.Fatal("loopback HTTP set a __Host- cookie")
	}
	txn := decodeLoginTransaction(t, login.Value)
	issuer.claims = map[string]any{"nonce": txn.Nonce, "groups": []string{"marketplace-admins"}}

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state="+txn.State+"&code=c", testSecureLoginCookie+"="+login.Value))
	if resp.Code != 401 {
		t.Fatalf("__Host- transaction cookie accepted on loopback HTTP: %d", resp.Code)
	}
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, adminRequest(http.MethodGet, "/api/v1/admin/auth/callback?state="+txn.State+"&code=c", adminLoginCookie+"="+login.Value))
	if resp.Code != 303 {
		t.Fatalf("callback: %d %s", resp.Code, resp.Body.String())
	}
	if expired := requireAdminCookie(t, resp, adminLoginCookie, adminAuthBase, false); expired.MaxAge >= 0 {
		t.Fatalf("transaction cookie not expired: %+v", expired)
	}
	session := requireAdminCookie(t, resp, adminSessionCookie, adminCookiePath, false)
	if session.Value == "" {
		t.Fatal("session cookie not set")
	}

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/session", adminSessionCookie+"="+session.Value))
	if w.Code != 200 {
		t.Fatalf("loopback session: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, adminRequest(http.MethodGet, "/api/v1/admin/session", testSecureSessionCookie+"="+session.Value))
	if w.Code != 401 {
		t.Fatalf("__Host- session cookie accepted on loopback HTTP: %d", w.Code)
	}

	r := adminRequest(http.MethodPost, "/api/v1/admin/auth/logout", adminSessionCookie+"="+session.Value)
	r.Header.Set("Origin", "http://localhost:8080")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Marketplace-Admin", "1")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("logout: %d %s", w.Code, w.Body.String())
	}
	if expired := requireAdminCookie(t, w, adminSessionCookie, adminCookiePath, false); expired.MaxAge >= 0 {
		t.Fatalf("logout did not expire session: %+v", expired)
	}
	if expired := requireAdminCookie(t, w, adminLoginCookie, adminAuthBase, false); expired.MaxAge >= 0 {
		t.Fatalf("logout did not expire transaction: %+v", expired)
	}
}
