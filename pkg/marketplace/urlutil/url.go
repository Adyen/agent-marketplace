package urlutil

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// NormalizeEndpointURL validates and canonicalizes an HTTP(S) Marketplace
// endpoint URL. It is deliberately independent of the internal config and
// Marketplace packages so both consumers apply the same identity predicate.
func NormalizeEndpointURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("marketplace URL is required")
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid marketplace URL: %w", err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("marketplace URL must use http or https")
	}
	if u.Host == "" || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("marketplace URL must contain a host and no user info")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", fmt.Errorf("marketplace URL must not contain a query or fragment")
	}
	if unsafePath(u) {
		return "", fmt.Errorf("marketplace URL contains path traversal or an unsafe path")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func unsafePath(u *url.URL) bool {
	escapedPath := strings.ToLower(u.EscapedPath())
	if strings.ContainsAny(escapedPath, "\\") || strings.Contains(escapedPath, "%2f") || strings.Contains(escapedPath, "%5c") {
		return true
	}
	for _, r := range u.Path {
		if unicode.IsControl(r) {
			return true
		}
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." || strings.ContainsAny(segment, "\\") {
			return true
		}
	}
	return false
}
