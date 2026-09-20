package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Administration authentication modes. Exactly one mode is selected for an
// enabled administration edge; an empty mode is accepted only when the legacy
// trusted-proxy field set is complete, which infers trusted-proxy mode for
// backward compatibility.
const (
	AdminAuthOIDC         = "oidc"
	AdminAuthTrustedProxy = "trusted_proxy"
)

const (
	defaultAdminSessionTTL = 12 * time.Hour
	maxAdminSessionTTL     = 24 * time.Hour
	minAdminSessionSecret  = 32
	maxAdminSessionSecret  = 4096
)

type Administration struct {
	Enabled                bool                `yaml:"enabled"`
	AllowInsecureLocalhost bool                `yaml:"allow_insecure_localhost"`
	TrustedProxies         []string            `yaml:"trusted_proxies"`
	Origin                 string              `yaml:"origin"`
	Issuer                 string              `yaml:"issuer"`
	SubjectHeader          string              `yaml:"subject_header"`
	GroupsHeader           string              `yaml:"groups_header"`
	AdminGroups            []string            `yaml:"admin_groups"`
	AdminUsers             []string            `yaml:"admin_users"`
	Authentication         AdminAuthentication `yaml:"authentication"`
}

// AdminAuthentication selects the administration identity provider mode.
type AdminAuthentication struct {
	Mode string    `yaml:"mode"`
	OIDC AdminOIDC `yaml:"oidc"`
}

// AdminOIDC configures the dedicated public OIDC client used for native
// administration sign-in. The session signing secret is accepted only by
// environment or file reference; inline secrets are not supported.
type AdminOIDC struct {
	Issuer            string   `yaml:"issuer"`
	ClientID          string   `yaml:"client_id"`
	SessionSecretEnv  string   `yaml:"session_secret_env"`
	SessionSecretFile string   `yaml:"session_secret_file"`
	GroupsClaim       string   `yaml:"groups_claim"`
	SessionTTL        Duration `yaml:"session_ttl"`
}

func (o AdminOIDC) configured() bool {
	return o.Issuer != "" || o.ClientID != "" || o.SessionSecretEnv != "" || o.SessionSecretFile != "" || o.GroupsClaim != "" || o.SessionTTL != 0
}

func (a Administration) proxyFieldsPresent() bool {
	return len(a.TrustedProxies) > 0 || a.Issuer != "" || a.SubjectHeader != "" || a.GroupsHeader != ""
}

func (a Administration) proxyFieldsComplete() bool {
	return len(a.TrustedProxies) > 0 && strings.TrimSpace(a.Issuer) != "" && a.SubjectHeader != "" && a.GroupsHeader != ""
}

// AuthMode resolves the effective authentication mode. An enabled legacy
// configuration with no explicit mode and a complete trusted-proxy field set
// infers trusted-proxy mode; anything else requires an explicit mode.
func (a Administration) AuthMode() string {
	mode := strings.TrimSpace(a.Authentication.Mode)
	if mode == "" && a.proxyFieldsComplete() {
		return AdminAuthTrustedProxy
	}
	return mode
}

// ResolvedOIDC returns the OIDC settings with documented defaults applied.
func (a Administration) ResolvedOIDC() AdminOIDC {
	o := a.Authentication.OIDC
	if o.GroupsClaim == "" {
		o.GroupsClaim = "groups"
	}
	if o.SessionTTL == 0 {
		o.SessionTTL = Duration(defaultAdminSessionTTL)
	}
	return o
}

// SessionLifetime returns the bounded effective admin session lifetime.
func (a Administration) SessionLifetime() time.Duration {
	return a.ResolvedOIDC().SessionTTL.Duration()
}

var errAdminInvalid = errors.New("admin requires an HTTPS origin (or explicit allow_insecure_localhost for loopback HTTP), exactly one authentication mode, and at least one admin group or admin subject")

func (a Administration) Validate() error {
	if !a.Enabled {
		return nil
	}
	u, err := url.Parse(a.Origin)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(a.Origin, "#") || u.Opaque != "" {
		return errAdminInvalid
	}
	ip, _ := netip.ParseAddr(u.Hostname())
	localHTTP := a.AllowInsecureLocalhost && u.Scheme == "http" && (strings.EqualFold(u.Hostname(), "localhost") || ip.IsLoopback() && ip.Zone() == "")
	if u.Scheme != "https" && !localHTTP {
		return errAdminInvalid
	}
	if !canonicalAdminOrigin(a.Origin, u) {
		return errAdminInvalid
	}
	for _, group := range a.AdminGroups {
		if !validAdminListValue(group) {
			return errAdminInvalid
		}
	}
	for _, subject := range a.AdminUsers {
		if !validAdminListValue(subject) {
			return errAdminInvalid
		}
	}
	if len(a.AdminGroups) == 0 && len(a.AdminUsers) == 0 {
		return errAdminInvalid
	}
	switch mode := a.AuthMode(); mode {
	case AdminAuthOIDC:
		if a.proxyFieldsPresent() {
			return errors.New("admin OIDC mode rejects trusted-proxy identity fields")
		}
		return a.validateOIDC()
	case AdminAuthTrustedProxy:
		if a.Authentication.OIDC.configured() {
			return errors.New("admin trusted-proxy mode rejects OIDC fields")
		}
		return a.validateTrustedProxy()
	default:
		return errAdminInvalid
	}
}

func canonicalAdminOrigin(raw string, u *url.URL) bool {
	if u.Scheme != strings.ToLower(u.Scheme) {
		return false
	}
	host, ok := canonicalAdminHost(u.Hostname())
	if !ok {
		return false
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 || port != strconv.Itoa(number) {
			return false
		}
		if u.Scheme == "https" && port == "443" || u.Scheme == "http" && port == "80" {
			return false
		}
		host += ":" + port
	}
	return raw == u.Scheme+"://"+host
}

func canonicalAdminHost(hostname string) (string, bool) {
	if addr, err := netip.ParseAddr(hostname); err == nil {
		if addr.Zone() != "" || addr.Is4In6() || hostname != addr.String() {
			return "", false
		}
		if strings.Contains(hostname, ":") {
			return "[" + hostname + "]", true
		}
		return hostname, true
	}
	if !canonicalAdminDNSName(hostname) {
		return "", false
	}
	return hostname, true
}

func canonicalAdminDNSName(hostname string) bool {
	if len(hostname) == 0 || len(hostname) > 253 {
		return false
	}
	labels := strings.Split(hostname, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			char := label[i]
			if char != '-' && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
				return false
			}
		}
	}
	return !numericAdminLabel(labels[len(labels)-1])
}

func numericAdminLabel(label string) bool {
	rest, hex := strings.CutPrefix(label, "0x")
	if !hex {
		rest = label
	}
	if rest == "" {
		return false
	}
	for i := 0; i < len(rest); i++ {
		char := rest[i]
		if char < '0' || char > '9' && (!hex || char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func (a Administration) validateTrustedProxy() error {
	if !a.proxyFieldsComplete() || strings.ContainsAny(a.Issuer, "\x00\r\n") {
		return errAdminInvalid
	}
	for _, cidr := range a.TrustedProxies {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || prefix.Bits() == 0 || prefix.Addr().Is4In6() {
			return errAdminInvalid
		}
	}
	if !validIdentityHeader(a.SubjectHeader) || !validIdentityHeader(a.GroupsHeader) || strings.EqualFold(a.SubjectHeader, a.GroupsHeader) {
		return errors.New("admin requires distinct dedicated identity headers; X-Forwarded-* and reserved HTTP headers are not permitted")
	}
	return nil
}

var (
	oidcClaimNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,127}$`)
	envNamePattern       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func (a Administration) validateOIDC() error {
	o := a.Authentication.OIDC
	issuer, err := url.Parse(o.Issuer)
	if err != nil || issuer.Hostname() == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.ForceQuery || issuer.Fragment != "" || issuer.Opaque != "" || strings.HasSuffix(o.Issuer, "/") {
		return errAdminInvalid
	}
	issuerIP, _ := netip.ParseAddr(issuer.Hostname())
	issuerHTTP := a.AllowInsecureLocalhost && issuer.Scheme == "http" && (strings.EqualFold(issuer.Hostname(), "localhost") || issuerIP.IsLoopback() && issuerIP.Zone() == "")
	if issuer.Scheme != "https" && !issuerHTTP {
		return errAdminInvalid
	}
	if !validOIDCField(o.ClientID, 256) {
		return errAdminInvalid
	}
	if o.GroupsClaim != "" && !oidcClaimNamePattern.MatchString(o.GroupsClaim) {
		return errAdminInvalid
	}
	if ttl := o.SessionTTL.Duration(); ttl < 0 || ttl > maxAdminSessionTTL {
		return errAdminInvalid
	}
	envSet := strings.TrimSpace(o.SessionSecretEnv) != ""
	fileSet := strings.TrimSpace(o.SessionSecretFile) != ""
	if envSet == fileSet {
		return errors.New("admin OIDC requires exactly one session secret reference (session_secret_env or session_secret_file)")
	}
	if envSet && !envNamePattern.MatchString(strings.TrimSpace(o.SessionSecretEnv)) {
		return errAdminInvalid
	}
	return nil
}

func validOIDCField(value string, max int) bool {
	if value == "" || len(value) > max || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if char < 32 || char == 127 {
			return false
		}
	}
	return true
}

func validAdminListValue(value string) bool {
	return validOIDCField(value, 512)
}

// ResolveSessionSecret resolves the configured session signing secret from its
// environment or file reference. The resolved value must be at least 32 bytes;
// the secret value is never included in errors.
func (a Administration) ResolveSessionSecret() ([]byte, error) {
	invalid := errors.New("admin OIDC session secret reference is invalid or unresolved")
	o := a.Authentication.OIDC
	var secret []byte
	switch {
	case strings.TrimSpace(o.SessionSecretEnv) != "":
		name := strings.TrimSpace(o.SessionSecretEnv)
		if !envNamePattern.MatchString(name) {
			return nil, invalid
		}
		value, ok := os.LookupEnv(name)
		if !ok {
			return nil, fmt.Errorf("admin OIDC session_secret_env %q is unset", name)
		}
		secret = []byte(value)
	case strings.TrimSpace(o.SessionSecretFile) != "":
		value, err := os.ReadFile(strings.TrimSpace(o.SessionSecretFile))
		if err != nil {
			return nil, fmt.Errorf("read admin OIDC session secret: %w", err)
		}
		secret = value
	default:
		return nil, invalid
	}
	secret = []byte(strings.TrimSpace(string(secret)))
	if len(secret) < minAdminSessionSecret || len(secret) > maxAdminSessionSecret {
		return nil, invalid
	}
	return secret, nil
}

var identityHeaderPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)

func validIdentityHeader(value string) bool {
	lower := strings.ToLower(value)
	return identityHeaderPattern.MatchString(value) && !strings.HasPrefix(lower, "x-forwarded-") && !slices.Contains([]string{"host", "origin", "forwarded", "cookie", "authorization", "content-type", "content-length", "connection", "x-marketplace-admin", "if-match", "idempotency-key", "set-cookie", "referer", "traceparent", "upgrade", "expect", "te", "trailer"}, lower)
}

func SourceHarnesses() []string {
	harnesses := make([]string, 0, len(validSourceHarnesses))
	for name, accepted := range validSourceHarnesses {
		if accepted && name != "" {
			harnesses = append(harnesses, name)
		}
	}
	slices.Sort(harnesses)
	return harnesses
}
