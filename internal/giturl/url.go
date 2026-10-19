// Package giturl contains the canonical safety predicates for Git origins.
package giturl

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var refNameRE = regexp.MustCompile(`^[A-Za-z0-9._+/@-]+$`)

// ValidateRef rejects option-like refs, unsafe components, control characters,
// and Git extended syntax that could alter rev-parse behavior.
func ValidateRef(raw string) error {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return errors.New("registry ref is required")
	}
	if raw != ref {
		return errors.New("invalid git ref: leading or trailing whitespace")
	}
	if strings.HasPrefix(ref, "-") {
		return errors.New("invalid git ref: option-like")
	}
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "~") {
		return errors.New("invalid git ref: absolute or tilde path")
	}
	if strings.Contains(ref, "..") || strings.Contains(ref, "@{") {
		return errors.New("invalid git ref: contains '..' or '@{'")
	}
	for i := 0; i < len(ref); i++ {
		if ref[i] < 0x20 || ref[i] == 0x7f {
			return errors.New("invalid git ref: control character")
		}
	}
	if !refNameRE.MatchString(ref) {
		return errors.New("invalid git ref: invalid character")
	}
	if strings.HasSuffix(ref, "/") {
		return errors.New("invalid git ref: invalid suffix")
	}
	for _, component := range strings.Split(ref, "/") {
		if component == "" {
			return errors.New("invalid git ref: empty component")
		}
		if strings.HasPrefix(component, ".") {
			return errors.New("invalid git ref: component begins with '.'")
		}
		if strings.HasSuffix(component, ".") || strings.HasSuffix(component, ".lock") {
			return errors.New("invalid git ref: invalid suffix")
		}
	}
	return nil
}

// IsFullObjectID reports whether ref names a commit exactly: a full SHA-1 (40)
// or SHA-256 (64) object ID in either letter case. Abbreviated IDs are not
// exact, because Git prefers a ref with the same name.
func IsFullObjectID(ref string) bool {
	if len(ref) != 40 && len(ref) != 64 {
		return false
	}
	for i := 0; i < len(ref); i++ {
		c := ref[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// ValidateURL rejects option-like URLs, control characters, traversal patterns,
// and unsupported or dangerous transport protocols while allowing the
// protocols used by the project (ssh, https, http, file) and local paths.
func ValidateURL(raw string) error {
	for i := 0; i < len(raw); i++ {
		if raw[i] < 0x20 || raw[i] == 0x7f {
			return errors.New("invalid git URL: control character")
		}
	}
	if strings.Contains(raw, `\`) {
		return errors.New("invalid git URL: backslash is not allowed")
	}

	u := strings.TrimSpace(raw)
	if u == "" {
		return errors.New("registry url is required")
	}
	if raw != u {
		return errors.New("invalid git URL: leading or trailing whitespace")
	}
	if strings.HasPrefix(u, "-") {
		return errors.New("invalid git URL: option-like")
	}
	if strings.Contains(u, "..") {
		return errors.New("invalid git URL: path traversal")
	}

	lower := strings.ToLower(u)
	if strings.Contains(lower, "ext::") || strings.Contains(lower, "remote::") || strings.Contains(lower, "helper::") {
		return errors.New("invalid git URL: unsupported transport helper")
	}

	if scheme, rest, ok := strings.Cut(u, "://"); ok {
		scheme = strings.ToLower(scheme)
		switch scheme {
		case "https", "http", "ssh", "file":
			// Allowed protocols.
		default:
			if scheme == "git" {
				return errors.New("invalid git URL: git:// protocol is not allowed")
			}
			return fmt.Errorf("invalid git URL: unsupported protocol %q", scheme)
		}

		// Reject hosts that start with '-', which can be interpreted as ssh/curl
		// command-line options (e.g. ssh://-oProxyCommand=...).
		if scheme != "file" {
			auth := rest
			if i := strings.IndexByte(auth, '/'); i >= 0 {
				auth = auth[:i]
			}
			if i := strings.LastIndexByte(auth, '@'); i >= 0 {
				auth = auth[i+1:]
			}
			if i := strings.LastIndexByte(auth, ':'); i > 0 {
				auth = auth[:i]
			}
			if auth == "" || strings.HasPrefix(auth, "-") {
				return errors.New("invalid git URL: host starts with '-'")
			}
		}
		return nil
	}

	if strings.Contains(lower, "::") {
		return errors.New("invalid git URL: unsupported transport helper")
	}

	// Scp-style origins can omit the user (example.com:org/repo.git). Treat
	// both forms uniformly so an option-like path cannot reach ssh or git.
	if at := strings.IndexByte(u, '@'); at >= 0 {
		u = u[at+1:]
	}
	if colon := strings.IndexByte(u, ':'); colon > 0 {
		host, path := u[:colon], u[colon+1:]
		if host == "" || strings.HasPrefix(host, "-") {
			return errors.New("invalid git URL: host starts with '-'")
		}
		if strings.HasPrefix(path, "-") {
			return errors.New("invalid git URL: path starts with '-'")
		}
		return nil
	}

	if strings.HasPrefix(u, "~") {
		return errors.New("invalid git URL: tilde path is not allowed")
	}

	return nil
}
