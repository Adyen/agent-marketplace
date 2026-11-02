//go:build !e2e

package guiupdate

import "net/url"

func validGitHubURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() == "github.com" && parsed.Port() == "" && parsed.User == nil
}
