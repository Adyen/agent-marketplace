package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/redact"
)

// RegistryURLBlockedError is returned by ValidateRegistryURLs when one or more
// non-local registry URLs do not match any pattern in the allowed_registries
// policy.
type RegistryURLBlockedError struct {
	BlockedURLs     []string
	AllowedPatterns []string
}

func (e *RegistryURLBlockedError) Error() string {
	if e == nil {
		return "registry URL blocked by allowed_registries policy"
	}
	return fmt.Sprintf("registry URL(s) blocked by allowed_registries policy: %s (allowed patterns: %s)",
		strings.Join(redact.Strings(e.BlockedURLs), ", "), strings.Join(redact.Strings(e.AllowedPatterns), ", "))
}

// IsRegistryURLBlockedError reports whether err is or wraps a
// RegistryURLBlockedError.
func IsRegistryURLBlockedError(err error) bool {
	var blockedErr *RegistryURLBlockedError
	return errors.As(err, &blockedErr)
}

// OnlyRegistryURLBlockedErrors reports whether every error in errs is a
// RegistryURLBlockedError (and the slice is non-empty).
func OnlyRegistryURLBlockedErrors(errs []error) bool {
	if len(errs) == 0 {
		return false
	}
	for _, err := range errs {
		if !IsRegistryURLBlockedError(err) {
			return false
		}
	}
	return true
}

// ValidateRegistryURLs checks that every non-local registry entry's URL
// matches at least one pattern in patterns. Returns nil if all URLs pass
// or if patterns is nil (nil means no allowlist is configured, so all remote
// origins are permitted). A non-nil empty slice means the allowlist is
// explicitly empty, so all non-local URLs are denied. Returns an error listing
// all blocked URLs.
func ValidateRegistryURLs(entries []RegistryEntry, patterns []string) error {
	if patterns == nil {
		return nil
	}

	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("invalid allowed_registries pattern %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}

	var blocked []string
	for _, entry := range entries {
		if entry.IsLocal() {
			continue
		}
		url := strings.TrimSpace(entry.URL)
		if url == "" {
			continue
		}
		normalized := normalizeRegistryURL(url)
		if !matchesAny(normalized, compiled) {
			blocked = append(blocked, url)
		}
	}

	if len(blocked) == 0 {
		return nil
	}

	return &RegistryURLBlockedError{BlockedURLs: blocked, AllowedPatterns: patterns}
}

// normalizeRegistryURL reduces a git remote URL to host/path form.
// "https://example.com/team/repo.git" → "example.com/team/repo"
// "git@example.com:team/repo.git"     → "example.com/team/repo"
// "example.com/team/repo.git"          → "example.com/team/repo"
func normalizeRegistryURL(url string) string {
	// Strip common scheme prefixes.
	for _, prefix := range []string{"https://", "http://", "git://", "ssh://"} {
		if strings.HasPrefix(url, prefix) {
			url = strings.TrimPrefix(url, prefix)
			break
		}
	}
	// Strip git@ prefix (SSH shorthand): "git@host:path" → "host/path".
	if strings.HasPrefix(url, "git@") {
		url = strings.TrimPrefix(url, "git@")
		url = strings.Replace(url, ":", "/", 1)
	}
	// Strip trailing slashes, then .git suffix (handles ".git/" case).
	url = strings.TrimRight(url, "/")
	url = strings.TrimSuffix(url, ".git")
	// Ensure path separator exists so host-only patterns can distinguish
	// example.com/repo from evil.example.com.evil.com/repo.
	// For bare host URLs (e.g. "example.com"), append "/".
	if !strings.Contains(url, "/") {
		url += "/"
	}
	return url
}

func matchesAny(s string, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
