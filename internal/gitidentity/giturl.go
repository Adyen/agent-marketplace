package gitidentity

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var scpOrigin = regexp.MustCompile(`^([^/@:[:space:]]+)@([^/:[:space:]]+):(.+)$`)

func CanonicalV1(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	isFileSpelling := strings.HasPrefix(strings.ToLower(raw), "file://")
	if raw == "" || HasASCIIControl(raw) {
		return "", fmt.Errorf("empty URL or control character")
	}
	if matches := scpOrigin.FindStringSubmatch(raw); matches != nil {
		return canonicalSCP(matches[1], matches[2], matches[3])
	}
	// net/url represents a literal trailing fragment marker as an empty
	// Fragment, so checking Fragment alone would silently accept "#".
	if strings.HasSuffix(raw, "#") {
		return "", fmt.Errorf("empty fragments are forbidden")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse: %v", err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", fmt.Errorf("query strings and fragments are forbidden")
	}
	if u.User != nil {
		if _, set := u.User.Password(); set {
			return "", fmt.Errorf("URL passwords are forbidden")
		}
	}
	switch u.Scheme {
	case "http", "https", "ssh":
		return canonicalRemote(u)
	case "git+scp":
		if u.User == nil || u.Port() != "" || u.Path == "" || !strings.HasPrefix(u.Path, "/") {
			return "", fmt.Errorf("invalid canonical SCP origin")
		}
		return canonicalSCP(u.User.Username(), u.Hostname(), strings.TrimPrefix(u.EscapedPath(), "/"))
	case "file":
		if !isFileSpelling {
			return "", fmt.Errorf("file URL must use file:// absolute syntax")
		}
		return canonicalFile(u)
	default:
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
}

func canonicalRemote(u *url.URL) (string, error) {
	if u.Host == "" || u.Hostname() == "" {
		return "", fmt.Errorf("host is required")
	}
	if u.Path == "" || !strings.HasPrefix(u.Path, "/") {
		return "", fmt.Errorf("absolute repository path is required")
	}
	repositoryPath, err := canonicalRepositoryPath(u.EscapedPath(), true)
	if err != nil {
		return "", err
	}
	host := strings.ToLower(u.Hostname())
	if !validRemoteHost(host) {
		return "", fmt.Errorf("invalid remote host")
	}
	port := u.Port()
	if port != "" {
		if _, err := net.LookupPort("tcp", port); err != nil {
			return "", fmt.Errorf("invalid port %q", port)
		}
		if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") || (u.Scheme == "ssh" && port == "22") {
			port = ""
		}
	}
	hostPort := host
	if strings.Contains(host, ":") {
		hostPort = "[" + host + "]"
	}
	if port != "" {
		hostPort = net.JoinHostPort(host, port)
	}
	userinfo := ""
	if u.Scheme == "ssh" && u.User != nil {
		username := u.User.Username()
		if username == "" || HasASCIIControl(username) {
			return "", fmt.Errorf("invalid SSH username")
		}
		userinfo = url.User(username).String() + "@"
	}
	return u.Scheme + "://" + userinfo + hostPort + repositoryPath, nil
}

func canonicalSCP(user, host, repositoryPath string) (string, error) {
	if strings.HasPrefix(repositoryPath, "/") {
		return "", fmt.Errorf("SCP repository path must be relative")
	}
	if strings.ContainsAny(user, "?#") || strings.ContainsAny(host, "?#@") || HasASCIIControl(user+host) || !validRemoteHost(strings.ToLower(host)) {
		return "", fmt.Errorf("invalid SCP origin")
	}
	cleaned, err := canonicalRepositoryPath(repositoryPath, false)
	if err != nil {
		return "", err
	}
	return "git+scp://" + url.User(user).String() + "@" + strings.ToLower(host) + "/" + cleaned, nil
}

func canonicalFile(u *url.URL) (string, error) {
	if u.Host != "" || u.User != nil || u.Opaque != "" {
		return "", fmt.Errorf("file URL must be local and hierarchical")
	}
	if u.Path == "" || !strings.HasPrefix(u.Path, "/") {
		return "", fmt.Errorf("file URL path must be absolute")
	}
	cleaned, err := canonicalRepositoryPath(u.EscapedPath(), true)
	if err != nil {
		return "", err
	}
	return "file://" + cleaned, nil
}

func canonicalRepositoryPath(escaped string, absolute bool) (string, error) {
	lower := strings.ToLower(escaped)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return "", fmt.Errorf("encoded separators are forbidden")
	}
	decoded, err := url.PathUnescape(escaped)
	if err != nil || HasASCIIControl(decoded) || strings.Contains(decoded, `\`) {
		return "", fmt.Errorf("invalid repository path")
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("dot segments are forbidden")
		}
	}
	cleaned := path.Clean(decoded)
	if absolute && !strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("absolute path is required")
	}
	if !absolute {
		cleaned = strings.TrimPrefix(cleaned, "/")
	}
	cleaned = strings.TrimSuffix(cleaned, "/")
	cleaned = strings.TrimSuffix(cleaned, ".git")
	if cleaned == "" || cleaned == "." || cleaned == "/" {
		return "", fmt.Errorf("repository path is required")
	}
	segments := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	encoded := strings.Join(segments, "/")
	if absolute {
		return "/" + encoded, nil
	}
	return encoded, nil
}

func validRemoteHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if host == "" || len(host) > 253 {
		return false
	}
	checked := strings.TrimSuffix(host, ".")
	for _, label := range strings.Split(checked, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range []byte(label) {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

// HasASCIIControl reports whether value contains an ASCII control character.
func HasASCIIControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
