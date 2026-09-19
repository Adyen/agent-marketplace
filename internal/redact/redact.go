package redact

import (
	"regexp"
	"strings"
)

var (
	credentialRE    = regexp.MustCompile(`://[^/\s?#]*@`)
	scpCredentialRE = regexp.MustCompile(`^[^@\s/:]+:[^@\s]+@([^\s/:]+:)`)
	urlSuffixRE     = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://[^\s?#]*)([?#])[^\s]*`)
)

// URL removes embedded URL credentials while preserving the host and path.
func URL(value string) string {
	value = credentialRE.ReplaceAllString(value, "://***@")
	value = scpCredentialRE.ReplaceAllString(value, "***@$1")
	if !strings.ContainsAny(value, "?#") {
		return value
	}
	return urlSuffixRE.ReplaceAllString(value, "${1}${2}***")
}

// Strings returns a copy with embedded URL credentials removed.
func Strings(values []string) []string {
	redacted := make([]string, len(values))
	for i, value := range values {
		redacted[i] = URL(value)
	}
	return redacted
}
