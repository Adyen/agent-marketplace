package registryid

import (
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SanitizeRegistryTelemetry returns the stable identity and display location
// permitted in telemetry. Local locations collapse completely to "local".
func SanitizeRegistryTelemetry(raw string) (identity, location string, local, ok bool) {
	if !utf8.ValidString(raw) || containsControl(raw) {
		return "", "", false, false
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false, false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(decoded) || containsControl(decoded) {
		return "", "", false, false
	}

	lower := strings.ToLower(raw)
	if raw == "." || raw == ".." || raw == "~" || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") || strings.HasPrefix(raw, "~/") || filepath.IsAbs(raw) || strings.HasPrefix(lower, "file:") {
		return "local", "local", true, true
	}

	if !strings.Contains(raw, "://") {
		return sanitizeSCPTelemetry(raw)
	}
	return sanitizeURLTelemetry(raw)
}

func sanitizeSCPTelemetry(raw string) (identity, location string, local, ok bool) {
	base := raw
	if cut := strings.IndexAny(base, "?#"); cut >= 0 {
		base = base[:cut]
	}
	if strings.Count(base, "@") > 1 {
		return "", "", false, false
	}
	if at := strings.IndexByte(base, '@'); at >= 0 {
		if at == 0 {
			return "", "", false, false
		}
		base = base[at+1:]
	}
	colon := strings.IndexByte(base, ':')
	if colon <= 0 || colon == len(base)-1 {
		return "", "", false, false
	}
	host := base[:colon]
	rawPath := base[colon+1:]
	if strings.ContainsAny(host, "/\\@%[]") || strings.ContainsAny(host, " \t\r\n") {
		return "", "", false, false
	}
	decodedPath, err := url.PathUnescape(strings.Trim(rawPath, "/"))
	if err != nil || decodedPath == "" || containsControl(decodedPath) {
		return "", "", false, false
	}
	identityPath := canonicalRegistryPath(decodedPath)
	if identityPath == "" {
		return "", "", false, false
	}
	host = strings.ToLower(host)
	return host + "/" + identityPath, host + ":" + rawPath, false, true
}

func sanitizeURLTelemetry(raw string) (identity, location string, local, ok bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Opaque != "" || parsed.Hostname() == "" {
		return "", "", false, false
	}
	path, err := url.PathUnescape(strings.Trim(parsed.EscapedPath(), "/"))
	if err != nil || path == "" || containsControl(path) {
		return "", "", false, false
	}
	identityPath := canonicalRegistryPath(path)
	if identityPath == "" {
		return "", "", false, false
	}

	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	displayHost := registryHost(hostname, port)
	identityHost := displayHost
	if isDefaultRegistryPort(strings.ToLower(parsed.Scheme), port) {
		identityHost = registryHost(hostname, "")
	}
	parsed.User = nil
	parsed.Host = displayHost
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.Path = "/" + path
	parsed.RawPath = ""
	return identityHost + "/" + identityPath, strings.TrimSuffix(parsed.String(), "/"), false, true
}

func registryHost(hostname, port string) string {
	if port != "" {
		return net.JoinHostPort(hostname, port)
	}
	if strings.Contains(hostname, ":") {
		return "[" + hostname + "]"
	}
	return hostname
}

func isDefaultRegistryPort(scheme, port string) bool {
	return scheme == "ssh" && port == "22" || scheme == "https" && port == "443" || scheme == "http" && port == "80" || scheme == "git" && port == "9418"
}

func canonicalRegistryPath(value string) string {
	value = strings.ToLower(strings.Trim(value, "/"))
	value = strings.TrimSuffix(value, ".git")
	return strings.Trim(value, "/")
}

func containsControl(value string) bool {
	return strings.ContainsFunc(value, unicode.IsControl)
}
