//go:build e2e

package localapi

import (
	"net"
	"net/url"
	"os"
	"path"
	"strings"
)

func releaseEndpoint() string {
	if endpoint := e2eReleaseOrigin(); endpoint != nil {
		return os.Getenv("AGENT_MARKETPLACE_E2E_RELEASE_ENDPOINT")
	}
	return githubReleaseURL
}

func allowedReleasePageURL(raw string) bool {
	if isGitHubReleaseURL(raw) {
		return true
	}
	parsed, ok := sameE2EReleaseOrigin(raw)
	if !ok {
		return false
	}
	tag := strings.TrimPrefix(parsed.Path, "/Adyen/agent-marketplace/releases/tag/")
	return tag != "" && !strings.Contains(tag, "/") && parsed.RawQuery == "" && parsed.Fragment == ""
}

func allowedReleaseAssetURL(raw, tag, name string) bool {
	if isGitHubReleaseAssetURL(raw, tag, name) {
		return true
	}
	parsed, ok := sameE2EReleaseOrigin(raw)
	return ok && parsed.RawQuery == "" && parsed.Fragment == "" &&
		parsed.Path == path.Join("/Adyen/agent-marketplace/releases/download", tag, name)
}

func sameE2EReleaseOrigin(raw string) (*url.URL, bool) {
	endpoint := e2eReleaseOrigin()
	parsed, err := url.Parse(raw)
	if endpoint == nil || err != nil || parsed.User != nil {
		return nil, false
	}
	if parsed.Scheme != endpoint.Scheme || !strings.EqualFold(parsed.Host, endpoint.Host) {
		return nil, false
	}
	return parsed, true
}

func e2eReleaseOrigin() *url.URL {
	endpoint, err := url.Parse(os.Getenv("AGENT_MARKETPLACE_E2E_RELEASE_ENDPOINT"))
	if err != nil || endpoint == nil || endpoint.User != nil || endpoint.Host == "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
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
