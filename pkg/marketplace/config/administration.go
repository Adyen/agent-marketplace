package config

import (
	"errors"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

type Administration struct {
	Enabled        bool     `yaml:"enabled"`
	TrustedProxies []string `yaml:"trusted_proxies"`
	Origin         string   `yaml:"origin"`
	Issuer         string   `yaml:"issuer"`
	SubjectHeader  string   `yaml:"subject_header"`
	GroupsHeader   string   `yaml:"groups_header"`
	AdminGroups    []string `yaml:"admin_groups"`
}

func (a Administration) Validate() error {
	if !a.Enabled {
		return nil
	}
	invalid := errors.New("admin requires an HTTPS origin, explicit trusted proxy CIDRs, issuer, distinct identity headers and admin groups")
	u, err := url.Parse(a.Origin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(a.Origin, "#") || u.Opaque != "" {
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
	validHeader := func(value string) bool {
		if value == "" {
			return false
		}
		for _, c := range value {
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c) {
				continue
			}
			return false
		}
		return true
	}
	if !validHeader(a.SubjectHeader) || !validHeader(a.GroupsHeader) || strings.EqualFold(a.SubjectHeader, a.GroupsHeader) {
		return invalid
	}
	for _, group := range a.AdminGroups {
		if strings.TrimSpace(group) == "" || strings.TrimSpace(group) != group || strings.ContainsAny(group, "\x00\r\n") {
			return invalid
		}
	}
	return nil
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
