//go:build e2e

package guiupdate

import (
	"net"
	"net/url"
	"os"
	"strings"
)

func validGitHubURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return false
	}
	if parsed.Scheme == "https" && parsed.Hostname() == "github.com" && parsed.Port() == "" {
		return true
	}
	return sameE2EReleaseOrigin(parsed) && parsed.RawQuery == "" && parsed.Fragment == ""
}

// ValidReleasePageURL reports whether raw is an allowed release page URL: a
// GitHub URL, or the release tag page below the configured E2E origin.
func ValidReleasePageURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return false
	}
	if parsed.Scheme == "https" && parsed.Hostname() == "github.com" && parsed.Port() == "" {
		return true
	}
	if !sameE2EReleaseOrigin(parsed) {
		return false
	}
	tag := strings.TrimPrefix(parsed.Path, "/Adyen/agent-marketplace/releases/tag/")
	return tag != "" && !strings.Contains(tag, "/") && parsed.RawQuery == "" && parsed.Fragment == ""
}

// E2EReleaseEndpoint returns the configured E2E release origin when it is a
// valid loopback endpoint, or nil.
func E2EReleaseEndpoint() *url.URL {
	endpoint, err := url.Parse(os.Getenv("AGENT_MARKETPLACE_E2E_RELEASE_ENDPOINT"))
	if err != nil || endpoint == nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil
	}
	host := endpoint.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil
	}
	return endpoint
}

func sameE2EReleaseOrigin(parsed *url.URL) bool {
	endpoint := E2EReleaseEndpoint()
	return endpoint != nil && parsed.Scheme == endpoint.Scheme && strings.EqualFold(parsed.Host, endpoint.Host)
}
