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
	endpoint, err := url.Parse(os.Getenv("AGENT_MARKETPLACE_E2E_RELEASE_ENDPOINT"))
	if err != nil || endpoint == nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return false
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return false
	}
	host := endpoint.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return false
	}
	return parsed.Scheme == endpoint.Scheme && strings.EqualFold(parsed.Host, endpoint.Host) &&
		parsed.RawQuery == "" && parsed.Fragment == ""
}
