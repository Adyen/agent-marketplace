package api

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

type AdminAuthOptions = config.Administration

type adminActorKey struct{}

func adminActor(ctx context.Context) (store.ManagedActor, bool) {
	actor, ok := ctx.Value(adminActorKey{}).(store.ManagedActor)
	return actor, ok
}

func newAdminGuard(options AdminAuthOptions, next http.Handler) (http.Handler, error) {
	invalid := errors.New("invalid administration trust configuration")
	options.Enabled = true
	if options.Validate() != nil || !validIdentityValue(options.Issuer) || next == nil {
		return nil, invalid
	}
	if !validIdentityHeader(options.SubjectHeader) || !validIdentityHeader(options.GroupsHeader) || strings.EqualFold(options.SubjectHeader, options.GroupsHeader) {
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
	limiter := adminRateLimiter{actors: make(map[string]adminRequestCount)}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		peer, parseErr := netip.ParseAddr(host)
		if err != nil || parseErr != nil || !slices.ContainsFunc(prefixes, func(prefix netip.Prefix) bool { return prefix.Contains(peer.Unmap()) }) {
			writeAdminError(w, http.StatusForbidden, "forbidden", "Administration requires a trusted identity proxy.")
			return
		}
		subject, subjectOK := singleAdminHeader(r, options.SubjectHeader)
		groupHeader, groupsOK := singleAdminHeader(r, options.GroupsHeader)
		var groups []string
		if !subjectOK || !validIdentityValue(subject) || !groupsOK || len(groupHeader) > 8192 || json.Unmarshal([]byte(groupHeader), &groups) != nil || len(groups) > 256 {
			writeAdminError(w, http.StatusUnauthorized, "unauthenticated", "Sign in through the configured identity provider.")
			return
		}
		for _, group := range groups {
			if !validIdentityValue(group) {
				writeAdminError(w, http.StatusUnauthorized, "unauthenticated", "The identity claim is invalid.")
				return
			}
		}
		if !slices.ContainsFunc(groups, func(group string) bool { return slices.Contains(options.AdminGroups, group) }) {
			writeAdminError(w, http.StatusForbidden, "forbidden", "Marketplace administration permission is required.")
			return
		}
		origin, originOK := singleAdminHeader(r, "Origin")
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead
		if (mutation || len(r.Header.Values("Origin")) > 0) && (!originOK || origin != options.Origin) {
			writeAdminError(w, http.StatusForbidden, "origin_rejected", "Use the configured same-origin administration interface.")
			return
		}
		if mutation {
			custom, ok := singleAdminHeader(r, "X-Marketplace-Admin")
			if !ok || custom != "1" {
				writeAdminError(w, http.StatusForbidden, "request_header_required", "The administration request header is required.")
				return
			}
			contentType, ok := singleAdminHeader(r, "Content-Type")
			mediaType, _, err := mime.ParseMediaType(contentType)
			if !ok || err != nil || mediaType != "application/json" {
				writeAdminError(w, http.StatusUnsupportedMediaType, "json_required", "Administration mutations require JSON.")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		if !limiter.allow(subject, mutation && r.URL.Path == "/api/v1/admin/previews", time.Now()) {
			w.Header().Set("Retry-After", "60")
			writeAdminError(w, http.StatusTooManyRequests, "rate_limited", "Administration request limit reached; retry later.")
			return
		}
		actor := store.ManagedActor{Issuer: options.Issuer, Subject: subject}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminActorKey{}, actor)))
	}), nil
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

var identityHeaderPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)

func validIdentityHeader(value string) bool {
	lower := strings.ToLower(value)
	return identityHeaderPattern.MatchString(value) && !strings.HasPrefix(lower, "x-forwarded-") && !slices.Contains([]string{"host", "origin", "forwarded", "cookie", "authorization", "content-type", "content-length", "connection", "x-marketplace-admin", "if-match", "idempotency-key", "set-cookie", "referer", "traceparent", "upgrade", "expect", "te", "trailer"}, lower)
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
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{code, message})
}
