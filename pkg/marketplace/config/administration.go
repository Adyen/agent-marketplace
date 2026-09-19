package config

import (
	"errors"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type Administration struct {
	Enabled                bool     `yaml:"enabled"`
	AllowInsecureLocalhost bool     `yaml:"allow_insecure_localhost"`
	TrustedProxies         []string `yaml:"trusted_proxies"`
	Origin                 string   `yaml:"origin"`
	Issuer                 string   `yaml:"issuer"`
	SubjectHeader          string   `yaml:"subject_header"`
	GroupsHeader           string   `yaml:"groups_header"`
	AdminGroups            []string `yaml:"admin_groups"`
}

func (a Administration) Validate() error {
	if !a.Enabled {
		return nil
	}
	invalid := errors.New("admin requires an HTTPS origin (or explicit allow_insecure_localhost for loopback HTTP), explicit trusted proxy CIDRs, issuer, distinct identity headers and admin groups")
	u, err := url.Parse(a.Origin)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(a.Origin, "#") || u.Opaque != "" {
		return invalid
	}
	ip, _ := netip.ParseAddr(u.Hostname())
	localHTTP := a.AllowInsecureLocalhost && u.Scheme == "http" && (strings.EqualFold(u.Hostname(), "localhost") || ip.IsLoopback() && ip.Zone() == "")
	if u.Scheme != "https" && !localHTTP {
		return invalid
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return invalid
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return invalid
	}
	if len(a.TrustedProxies) == 0 || len(a.AdminGroups) == 0 || strings.TrimSpace(a.Issuer) == "" || strings.ContainsAny(a.Issuer, "\x00\r\n") {
		return invalid
	}
	for _, cidr := range a.TrustedProxies {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || prefix.Bits() == 0 || prefix.Addr().Is4In6() {
			return invalid
		}
	}
	if !validIdentityHeader(a.SubjectHeader) || !validIdentityHeader(a.GroupsHeader) || strings.EqualFold(a.SubjectHeader, a.GroupsHeader) {
		return errors.New("admin requires distinct dedicated identity headers; X-Forwarded-* and reserved HTTP headers are not permitted")
	}
	for _, group := range a.AdminGroups {
		if strings.TrimSpace(group) == "" || strings.TrimSpace(group) != group || strings.ContainsAny(group, "\x00\r\n") {
			return invalid
		}
	}
	return nil
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
