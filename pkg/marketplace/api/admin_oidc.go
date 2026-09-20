package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
)

const (
	adminSessionCookie = "marketplace_admin_session"
	adminLoginCookie   = "marketplace_admin_login"
	adminAuthBase      = "/api/v1/admin/auth"
	adminCookiePath    = "/api/v1/admin"
	adminLoginTTL      = 10 * time.Minute
	adminClockSkew     = 5 * time.Minute
	maxAdminCookie     = 3900
	maxAdminGroups     = 256
)

var adminOIDCScopes = []string{"openid", "profile", "email", "groups"}

const maxAdminOIDCResponse = 1 << 20

type adminOIDCUnavailableError struct {
	err error
}

func (e *adminOIDCUnavailableError) Error() string {
	return "OIDC provider unavailable: " + e.err.Error()
}
func (e *adminOIDCUnavailableError) Unwrap() error { return e.err }

type adminOIDCTransport struct {
	base http.RoundTripper
}

func (t adminOIDCTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return nil, &adminOIDCUnavailableError{err: err}
	}
	redirectStatus := response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest
	unavailableStatus := response.StatusCode >= http.StatusBadRequest &&
		(request.Method != http.MethodPost || response.StatusCode != http.StatusBadRequest && response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden)
	if unavailableStatus {
		_ = response.Body.Close()
		return nil, &adminOIDCUnavailableError{err: errors.New("provider returned an unavailable status")}
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxAdminOIDCResponse+1))
	_ = response.Body.Close()
	if readErr != nil {
		return nil, &adminOIDCUnavailableError{err: readErr}
	}
	if len(body) > maxAdminOIDCResponse {
		return nil, &adminOIDCUnavailableError{err: errors.New("provider response exceeds maximum size")}
	}
	if !redirectStatus && !json.Valid(body) {
		return nil, &adminOIDCUnavailableError{err: errors.New("provider response is malformed")}
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	return response, nil
}

func adminOIDCUnavailable(err error) bool {
	var unavailable *adminOIDCUnavailableError
	return errors.As(err, &unavailable)
}

type adminOIDCVerificationKey struct{}

type adminOIDCVerification struct {
	unavailable bool
}

type adminOIDCKeySet struct {
	remote oidc.KeySet
}

func (k adminOIDCKeySet) VerifySignature(ctx context.Context, rawIDToken string) ([]byte, error) {
	payload, err := k.remote.VerifySignature(ctx, rawIDToken)
	if err != nil && adminOIDCUnavailable(err) {
		if verification, ok := ctx.Value(adminOIDCVerificationKey{}).(*adminOIDCVerification); ok {
			verification.unavailable = true
		}
	}
	return payload, err
}

// adminLoginTransaction is the signed transient cookie payload carrying the
// login attempt. It never leaves the browser and expires after 10 minutes.
type adminLoginTransaction struct {
	Version  int    `json:"v"`
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"pkce"`
	Next     string `json:"next"`
	IssuedAt int64  `json:"iat"`
	Expiry   int64  `json:"exp"`
}

// adminSessionPayload is the signed session cookie payload. It contains only
// verified identity claims; provider tokens and PKCE material are never stored.
type adminSessionPayload struct {
	Version  int      `json:"v"`
	Issuer   string   `json:"iss"`
	Subject  string   `json:"sub"`
	Groups   []string `json:"grp,omitempty"`
	Display  string   `json:"name,omitempty"`
	IssuedAt int64    `json:"iat"`
	Expiry   int64    `json:"exp"`
}

// lazyOIDCDiscovery resolves the issuer's provider. Production uses lazy
// cached discovery so temporary issuer unavailability never blocks server
// startup.
type lazyOIDCDiscovery struct {
	issuer string
	client *http.Client

	mu       sync.Mutex
	resolved *oidc.Provider
}

func (d *lazyOIDCDiscovery) provider(ctx context.Context) (*oidc.Provider, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.resolved != nil {
		return d.resolved, nil
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, d.client), d.issuer)
	if err != nil {
		return nil, err
	}
	d.resolved = provider
	return provider, nil
}

// adminOIDCAuthenticator owns the OIDC login transaction, callback validation,
// and signed session verification for the administration edge.
type adminOIDCAuthenticator struct {
	options     config.Administration
	oidc        config.AdminOIDC
	secret      []byte
	secure      bool
	redirectURL string
	discovery   *lazyOIDCDiscovery
	client      *http.Client
	verifierMu  sync.Mutex
	idVerifier  *oidc.IDTokenVerifier
}

func newAdminOIDCAuthenticator(options config.Administration) (*adminOIDCAuthenticator, error) {
	options.Enabled = true
	if options.Validate() != nil || options.AuthMode() != config.AdminAuthOIDC {
		return nil, errors.New("invalid OIDC administration configuration")
	}
	secret, err := options.ResolveSessionSecret()
	if err != nil {
		return nil, errors.New("admin OIDC session secret is unavailable")
	}
	resolved := options.ResolvedOIDC()
	origin, err := url.Parse(options.Origin)
	if err != nil {
		return nil, errors.New("invalid OIDC administration configuration")
	}
	// The default transport honors HTTPS_PROXY; the timeout bounds every
	// discovery, JWKS, and token-endpoint call.
	client := &http.Client{Transport: adminOIDCTransport{base: http.DefaultTransport}, Timeout: 10 * time.Second}
	return &adminOIDCAuthenticator{
		options:     options,
		oidc:        resolved,
		secret:      secret,
		secure:      origin.Scheme == "https",
		redirectURL: options.Origin + adminAuthBase + "/callback",
		discovery:   &lazyOIDCDiscovery{issuer: resolved.Issuer, client: client},
		client:      client,
	}, nil
}

func (a *adminOIDCAuthenticator) oauthConfig(provider *oidc.Provider) *oauth2.Config {
	endpoint := provider.Endpoint()
	endpoint.AuthStyle = oauth2.AuthStyleInParams
	return &oauth2.Config{
		ClientID:    a.oidc.ClientID,
		Endpoint:    endpoint,
		RedirectURL: a.redirectURL,
		Scopes:      adminOIDCScopes,
	}
}

func (a *adminOIDCAuthenticator) verifier(provider *oidc.Provider) (*oidc.IDTokenVerifier, error) {
	a.verifierMu.Lock()
	defer a.verifierMu.Unlock()
	if a.idVerifier != nil {
		return a.idVerifier, nil
	}
	var metadata struct {
		JWKSURL string `json:"jwks_uri"`
	}
	if err := provider.Claims(&metadata); err != nil || metadata.JWKSURL == "" {
		return nil, errors.New("OIDC discovery metadata has no JWKS endpoint")
	}
	keys := oidc.NewRemoteKeySet(a.providerContext(context.Background()), metadata.JWKSURL)
	a.idVerifier = oidc.NewVerifier(a.oidc.Issuer, adminOIDCKeySet{remote: keys}, &oidc.Config{
		ClientID:             a.oidc.ClientID,
		SupportedSigningAlgs: []string{oidc.RS256},
		SkipExpiryCheck:      true,
	})
	return a.idVerifier, nil
}

func (a *adminOIDCAuthenticator) providerContext(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, a.client)
}

// adminCookiePathFor returns the approved host-only path scope for each signed
// cookie on explicit loopback HTTP: the transient login transaction is confined
// to the auth endpoints, while the session covers the administration API tree.
// Secure HTTPS deployments instead rely on the __Host- prefix contract, which
// requires Path=/ and forbids Domain, trading path confinement for
// browser-enforced host isolation.
func adminCookiePathFor(name string) string {
	if name == adminLoginCookie {
		return adminAuthBase
	}
	return adminCookiePath
}

func (a *adminOIDCAuthenticator) cookieName(logical string) string {
	if a.secure {
		return "__Host-" + logical
	}
	return logical
}

func (a *adminOIDCAuthenticator) cookiePath(logical string) string {
	if a.secure {
		return "/"
	}
	return adminCookiePathFor(logical)
}

func (a *adminOIDCAuthenticator) cookie(logical, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     a.cookieName(logical),
		Value:    value,
		Path:     a.cookiePath(logical),
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (a *adminOIDCAuthenticator) expireCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, a.cookie(name, "", -1))
}

// seal signs a versioned JSON payload as base64url(payload).base64url(hmac).
func (a *adminOIDCAuthenticator) seal(payload any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write(body)
	sealed := base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(sealed) > maxAdminCookie {
		return "", errors.New("admin cookie payload exceeds bounds")
	}
	return sealed, nil
}

// open verifies the signature in constant time and decodes the payload with
// unknown-field rejection. It never reveals which check failed.
func (a *adminOIDCAuthenticator) open(value string, out any) error {
	invalid := errors.New("invalid admin cookie")
	if len(value) == 0 || len(value) > maxAdminCookie {
		return invalid
	}
	body64, sig64, ok := strings.Cut(value, ".")
	if !ok {
		return invalid
	}
	body, err := base64.RawURLEncoding.DecodeString(body64)
	if err != nil {
		return invalid
	}
	sig, err := base64.RawURLEncoding.DecodeString(sig64)
	if err != nil {
		return invalid
	}
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return invalid
	}
	return nil
}

// validAdminTransactionWindow bounds the signed login transaction to a single
// ten-minute window: issued-at may not be in the future, expiry must be in the
// future, and the pair may not span more than the login TTL.
func validAdminTransactionWindow(issuedAt, expiry, now int64) bool {
	return issuedAt > 0 && issuedAt <= now && expiry > now && expiry > issuedAt &&
		expiry-issuedAt <= int64(adminLoginTTL.Seconds())
}

func validAdminIDTokenWindow(issuedAt, expiry, now time.Time) bool {
	return !issuedAt.IsZero() && !expiry.IsZero() &&
		!issuedAt.Before(now.Add(-adminLoginTTL-adminClockSkew)) &&
		!issuedAt.After(now.Add(adminClockSkew)) && expiry.After(issuedAt) &&
		!now.After(expiry.Add(adminClockSkew))
}

// validAdminNext accepts only /marketplace/admin and descendant paths.
func validAdminNext(values []string) (string, bool) {
	const fallback = "/marketplace/admin"
	if len(values) == 0 || len(values) == 1 && values[0] == "" {
		return fallback, true
	}
	if len(values) != 1 {
		return "", false
	}
	next := values[0]
	if len(next) > 2048 || next == fallback {
		return next, len(next) <= 2048
	}
	if !strings.HasPrefix(next, fallback+"/") {
		return "", false
	}
	if strings.ContainsAny(next, "\\?#%") || strings.Contains(next, "..") {
		return "", false
	}
	for _, char := range next {
		if char < 0x21 || char == 0x7f {
			return "", false
		}
	}
	return next, true
}

func adminRandom() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func adminAuthHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
}

func (a *adminOIDCAuthenticator) handleLogin(w http.ResponseWriter, r *http.Request) {
	adminAuthHeaders(w)
	if !adminRequestProtections(w, r, a.options) {
		return
	}
	next, ok := validAdminNext(r.URL.Query()["next"])
	if !ok {
		writeAdminError(w, http.StatusBadRequest, "authentication_request_invalid", "The sign-in destination is not an administration path.")
		return
	}
	provider, err := a.discovery.provider(a.providerContext(r.Context()))
	if err != nil {
		slog.Warn("admin OIDC discovery unavailable", "outcome", "authentication_unavailable")
		writeAdminError(w, http.StatusServiceUnavailable, "authentication_unavailable", "The identity provider is unavailable.")
		return
	}
	now := time.Now()
	transaction := adminLoginTransaction{
		Version:  1,
		State:    adminRandom(),
		Nonce:    adminRandom(),
		Next:     next,
		IssuedAt: now.Unix(),
		Expiry:   now.Add(adminLoginTTL).Unix(),
	}
	verifier := oauth2.GenerateVerifier()
	transaction.Verifier = verifier
	sealed, err := a.seal(transaction)
	if err != nil {
		writeAdminError(w, http.StatusServiceUnavailable, "admin_unavailable", "Administration authentication is unavailable.")
		return
	}
	http.SetCookie(w, a.cookie(adminLoginCookie, sealed, int(adminLoginTTL.Seconds())))
	url := a.oauthConfig(provider).AuthCodeURL(transaction.State,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", transaction.Nonce))
	slog.Info("admin OIDC login started", "outcome", "redirect")
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func (a *adminOIDCAuthenticator) handleCallback(w http.ResponseWriter, r *http.Request) {
	adminAuthHeaders(w)
	if !adminRequestProtections(w, r, a.options) {
		return
	}
	reject := func() {
		writeAdminError(w, http.StatusUnauthorized, "authentication_rejected", "Sign-in could not be completed.")
	}
	cookie, err := r.Cookie(a.cookieName(adminLoginCookie))
	var transaction adminLoginTransaction
	if err != nil || a.open(cookie.Value, &transaction) != nil || transaction.Version != 1 ||
		!validAdminTransactionWindow(transaction.IssuedAt, transaction.Expiry, time.Now().Unix()) ||
		!validIdentityValue(transaction.State) ||
		!validIdentityValue(transaction.Nonce) || !validIdentityValue(transaction.Verifier) {
		reject()
		return
	}
	if _, ok := validAdminNext([]string{transaction.Next}); !ok || transaction.Next == "" {
		reject()
		return
	}
	// The transaction is single-use: expire it before attempting the exchange.
	a.expireCookie(w, adminLoginCookie)
	query := r.URL.Query()
	if len(query["error"]) > 0 {
		reject()
		return
	}
	states, codes := query["state"], query["code"]
	if len(states) != 1 || len(codes) != 1 || len(states[0]) > 512 || len(codes[0]) == 0 || len(codes[0]) > 4096 {
		writeAdminError(w, http.StatusBadRequest, "authentication_request_invalid", "The identity provider response is invalid.")
		return
	}
	if subtle.ConstantTimeCompare([]byte(states[0]), []byte(transaction.State)) != 1 {
		reject()
		return
	}
	ctx := a.providerContext(r.Context())
	provider, err := a.discovery.provider(ctx)
	if err != nil {
		writeAdminError(w, http.StatusServiceUnavailable, "authentication_unavailable", "The identity provider is unavailable.")
		return
	}
	token, err := a.oauthConfig(provider).Exchange(ctx, codes[0], oauth2.VerifierOption(transaction.Verifier))
	if err != nil {
		var retrieve *oauth2.RetrieveError
		if errors.As(err, &retrieve) && !adminOIDCUnavailable(err) {
			reject()
			return
		}
		slog.Warn("admin OIDC token endpoint unavailable", "outcome", "authentication_unavailable")
		writeAdminError(w, http.StatusServiceUnavailable, "authentication_unavailable", "The identity provider is unavailable.")
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		reject()
		return
	}
	// go-oidc performs exactly one bounded remote JWKS refetch inside Verify
	// when the token's key ID is absent from the cached key set; it exposes no
	// typed unknown-kid error, so a further retry here would re-run discovery
	// and JWKS for unrelated failures. Verification is therefore single-shot
	// and fails closed.
	verifier, err := a.verifier(provider)
	if err != nil {
		writeAdminError(w, http.StatusServiceUnavailable, "authentication_unavailable", "The identity provider is unavailable.")
		return
	}
	verification := &adminOIDCVerification{}
	idToken, err := verifier.Verify(context.WithValue(ctx, adminOIDCVerificationKey{}, verification), rawIDToken)
	if err != nil && verification.unavailable {
		slog.Warn("admin OIDC JWKS unavailable", "outcome", "authentication_unavailable")
		writeAdminError(w, http.StatusServiceUnavailable, "authentication_unavailable", "The identity provider is unavailable.")
		return
	}
	if err != nil || idToken == nil {
		slog.Warn("admin OIDC token rejected", "outcome", "authentication_rejected")
		reject()
		return
	}
	now := time.Now()
	if idToken.Nonce != transaction.Nonce || idToken.Issuer != a.oidc.Issuer ||
		!slices.Contains(idToken.Audience, a.oidc.ClientID) ||
		!validIdentityValue(idToken.Subject) ||
		!validAdminIDTokenWindow(idToken.IssuedAt, idToken.Expiry, now) {
		reject()
		return
	}
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		reject()
		return
	}
	if !validAuthorizedParty(claims, idToken.Audience, a.oidc.ClientID) {
		reject()
		return
	}
	groups, ok := claimGroups(claims, a.oidc.GroupsClaim)
	if !ok {
		reject()
		return
	}
	display, _ := claims["name"].(string)
	if display == "" {
		display, _ = claims["preferred_username"].(string)
	}
	if display != "" && !validIdentityValue(display) {
		display = ""
	}
	if !adminAuthorized(a.options, idToken.Subject, groups) {
		writeAdminError(w, http.StatusForbidden, "forbidden", "Marketplace administration permission is required.")
		return
	}
	now = time.Now()
	sealed, err := a.seal(adminSessionPayload{
		Version: 1, Issuer: idToken.Issuer, Subject: idToken.Subject, Groups: groups,
		Display: display, IssuedAt: now.Unix(), Expiry: now.Add(a.options.SessionLifetime()).Unix(),
	})
	if err != nil {
		reject()
		return
	}
	http.SetCookie(w, a.cookie(adminSessionCookie, sealed, int(a.options.SessionLifetime().Seconds())))
	slog.Info("admin OIDC login completed", "outcome", "session_issued")
	http.Redirect(w, r, transaction.Next, http.StatusSeeOther)
}

func validAuthorizedParty(claims map[string]any, audience []string, clientID string) bool {
	raw, present := claims["azp"]
	if !present {
		return len(audience) <= 1
	}
	azp, isString := raw.(string)
	return isString && validIdentityValue(azp) && azp == clientID
}

// claimGroups extracts the configured groups claim. An absent claim yields no
// groups; a present claim that is not a string array fails closed.
func claimGroups(claims map[string]any, claim string) ([]string, bool) {
	raw, present := claims[claim]
	if !present || raw == nil {
		return nil, true
	}
	list, isList := raw.([]any)
	if !isList {
		return nil, false
	}
	groups := make([]string, 0, len(list))
	for _, item := range list {
		value, isString := item.(string)
		if !isString {
			return nil, false
		}
		groups = append(groups, value)
	}
	return boundedClaimStrings(groups)
}

// boundedClaimStrings bounds a claim string list in count and per-entry value.
func boundedClaimStrings(values []string) ([]string, bool) {
	if len(values) > maxAdminGroups {
		return nil, false
	}
	for _, value := range values {
		if !validIdentityValue(value) {
			return nil, false
		}
	}
	return values, true
}

// handleLogout is an idempotent local sign-out. The shared mutation
// protections run before it; it never contacts the identity provider.
func (a *adminOIDCAuthenticator) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.expireCookie(w, adminSessionCookie)
	a.expireCookie(w, adminLoginCookie)
	slog.Info("admin OIDC logout", "outcome", "session_expired")
	writeAdminJSON(w, r, http.StatusOK, struct {
		Code string `json:"code"`
	}{"signed_out"})
}

// sessionIdentity is the OIDC-mode identity extractor for the shared guard.
func (a *adminOIDCAuthenticator) sessionIdentity(w http.ResponseWriter, r *http.Request) (adminIdentity, bool) {
	cookie, err := r.Cookie(a.cookieName(adminSessionCookie))
	var session adminSessionPayload
	now := time.Now().Unix()
	if err != nil || a.open(cookie.Value, &session) != nil || session.Version != 1 ||
		session.Issuer != a.oidc.Issuer || !validIdentityValue(session.Subject) ||
		session.Expiry <= now || session.IssuedAt > now+int64(adminClockSkew.Seconds()) ||
		len(session.Groups) > maxAdminGroups {
		writeAdminAuthError(w, "Sign in through the configured identity provider.", config.AdminAuthOIDC)
		return adminIdentity{}, false
	}
	for _, group := range session.Groups {
		if !validIdentityValue(group) {
			writeAdminAuthError(w, "The identity claim is invalid.", config.AdminAuthOIDC)
			return adminIdentity{}, false
		}
	}
	display := session.Display
	if display != "" && !validIdentityValue(display) {
		display = ""
	}
	return adminIdentity{Issuer: session.Issuer, Subject: session.Subject, Groups: session.Groups, Display: display}, true
}
