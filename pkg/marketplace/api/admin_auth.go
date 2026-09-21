package api

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type AdminAuthOptions = config.Administration

type adminActorKey struct{}

type adminDisplayKey struct{}

func adminActor(ctx context.Context) (store.ManagedActor, bool) {
	actor, ok := ctx.Value(adminActorKey{}).(store.ManagedActor)
	return actor, ok
}

func adminDisplay(ctx context.Context) string {
	display, _ := ctx.Value(adminDisplayKey{}).(string)
	return display
}

// adminIdentity is the provider-independent verified identity consumed by the
// shared administration guard.
type adminIdentity struct {
	Issuer  string
	Subject string
	Groups  []string
	Display string
}

// adminIdentityFunc extracts a verified identity or writes the authentication
// error and returns false.
type adminIdentityFunc func(w http.ResponseWriter, r *http.Request) (adminIdentity, bool)

func newAdminGuard(options AdminAuthOptions, next http.Handler) (http.Handler, error) {
	return newAdminGuardMode(options, nil, next)
}

func newAdminGuardMode(options AdminAuthOptions, oidc *adminOIDCAuthenticator, next http.Handler) (http.Handler, error) {
	invalid := errors.New("invalid administration trust configuration")
	options.Enabled = true
	if options.Validate() != nil || next == nil {
		return nil, invalid
	}
	var identify adminIdentityFunc
	switch options.AuthMode() {
	case config.AdminAuthOIDC:
		if oidc == nil {
			var err error
			oidc, err = newAdminOIDCAuthenticator(options)
			if err != nil {
				return nil, invalid
			}
		}
		identify = oidc.sessionIdentity
	case config.AdminAuthTrustedProxy:
		if !validIdentityValue(options.Issuer) {
			return nil, invalid
		}
		prefixes := make([]netip.Prefix, 0, len(options.TrustedProxies))
		for _, value := range options.TrustedProxies {
			prefix, err := netip.ParsePrefix(value)
			if err != nil || prefix.Bits() == 0 {
				return nil, invalid
			}
			prefixes = append(prefixes, prefix)
		}
		for _, group := range options.AdminGroups {
			if !validIdentityValue(group) {
				return nil, invalid
			}
		}
		identify = proxyAdminIdentity(options, prefixes)
	default:
		return nil, invalid
	}
	limiter := adminRateLimiter{actors: make(map[string]adminRequestCount)}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		identity, ok := identify(w, r)
		if !ok {
			return
		}
		if !adminAuthorized(options, identity.Subject, identity.Groups) {
			writeAdminError(w, http.StatusForbidden, "forbidden", "Marketplace administration permission is required.")
			return
		}
		if !adminRequestProtections(w, r, options) {
			return
		}
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead
		if !limiter.allow(identity.Subject, mutation && r.URL.Path == "/api/v1/admin/previews", time.Now()) {
			w.Header().Set("Retry-After", "60")
			writeAdminError(w, http.StatusTooManyRequests, "rate_limited", "Administration request limit reached; retry later.")
			return
		}
		actor := store.ManagedActor{Issuer: identity.Issuer, Subject: identity.Subject}
		ctx := context.WithValue(r.Context(), adminActorKey{}, actor)
		if identity.Display != "" {
			ctx = context.WithValue(ctx, adminDisplayKey{}, identity.Display)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	}), nil
}

func adminAuthorized(options AdminAuthOptions, subject string, groups []string) bool {
	if slices.Contains(options.AdminUsers, subject) {
		return true
	}
	return slices.ContainsFunc(groups, func(group string) bool { return slices.Contains(options.AdminGroups, group) })
}

// adminRequestProtections applies the provider-independent request controls:
// exact same-origin validation for mutations and any request carrying Origin,
// the administration request header, JSON-only mutation content type, and the
// one-megabyte mutation body limit. It writes the rejection and returns false.
func adminRequestProtections(w http.ResponseWriter, r *http.Request, options AdminAuthOptions) bool {
	origin, originOK := singleAdminHeader(r, "Origin")
	mutation := r.Method != http.MethodGet && r.Method != http.MethodHead
	if (mutation || len(r.Header.Values("Origin")) > 0) && (!originOK || origin != options.Origin) {
		writeAdminError(w, http.StatusForbidden, "origin_rejected", "Use the configured same-origin administration interface.")
		return false
	}
	if mutation {
		custom, ok := singleAdminHeader(r, "X-Marketplace-Admin")
		if !ok || custom != "1" {
			writeAdminError(w, http.StatusForbidden, "request_header_required", "The administration request header is required.")
			return false
		}
		contentType, ok := singleAdminHeader(r, "Content-Type")
		mediaType, _, err := mime.ParseMediaType(contentType)
		if !ok || err != nil || mediaType != "application/json" {
			writeAdminError(w, http.StatusUnsupportedMediaType, "json_required", "Administration mutations require JSON.")
			return false
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	}
	return true
}

func proxyAdminIdentity(options AdminAuthOptions, prefixes []netip.Prefix) adminIdentityFunc {
	return func(w http.ResponseWriter, r *http.Request) (adminIdentity, bool) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		peer, parseErr := netip.ParseAddr(host)
		if err != nil || parseErr != nil || !slices.ContainsFunc(prefixes, func(prefix netip.Prefix) bool { return prefix.Contains(peer.Unmap()) }) {
			writeAdminError(w, http.StatusForbidden, "forbidden", "Administration requires a trusted identity proxy.")
			return adminIdentity{}, false
		}
		subject, subjectOK := singleAdminHeader(r, options.SubjectHeader)
		groupHeader, groupsOK := singleAdminHeader(r, options.GroupsHeader)
		var groups []string
		if !subjectOK || !validIdentityValue(subject) || !groupsOK || len(groupHeader) > 8192 || json.Unmarshal([]byte(groupHeader), &groups) != nil || len(groups) > 256 {
			writeAdminAuthError(w, "Sign in through the configured identity provider.", config.AdminAuthTrustedProxy)
			return adminIdentity{}, false
		}
		for _, group := range groups {
			if !validIdentityValue(group) {
				writeAdminAuthError(w, "The identity claim is invalid.", config.AdminAuthTrustedProxy)
				return adminIdentity{}, false
			}
		}
		return adminIdentity{Issuer: options.Issuer, Subject: subject, Groups: groups}, true
	}
}

type adminRequestCount struct {
	Requests int
	Previews int
}

type adminRateLimiter struct {
	mu       sync.Mutex
	window   time.Time
	total    int
	previews int
	actors   map[string]adminRequestCount
}

func (l *adminRateLimiter) allow(subject string, preview bool, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.window.IsZero() || now.Sub(l.window) >= time.Minute {
		l.window = now
		l.total, l.previews = 0, 0
		clear(l.actors)
	}
	count := l.actors[subject]
	if l.total >= 600 || count.Requests >= 60 || (preview && (count.Previews >= 5 || l.previews >= 30)) {
		return false
	}
	count.Requests++
	l.total++
	if preview {
		count.Previews++
		l.previews++
	}
	l.actors[subject] = count
	return true
}

func singleAdminHeader(r *http.Request, name string) (string, bool) {
	values := r.Header.Values(name)
	if len(values) != 1 || values[0] == "" {
		return "", false
	}
	return values[0], true
}

func validIdentityValue(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if char < 32 || char == 127 {
			return false
		}
	}
	return true
}

func writeAdminError(w http.ResponseWriter, status int, code, message string) {
	writeAdminErrorFields(w, status, code, message, nil)
}

func writeAdminAuthError(w http.ResponseWriter, message, mode string) {
	writeAdminErrorFields(w, http.StatusUnauthorized, "unauthenticated", message, map[string]string{"authentication_mode": mode})
}

func writeAdminErrorFields(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields,omitempty"`
	}{code, message, fields})
}
