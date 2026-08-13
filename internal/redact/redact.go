package redact

import "regexp"

var credentialRE = regexp.MustCompile(`://[^/\s?#]*@`)

// URL removes embedded URL credentials while preserving the host and path.
func URL(value string) string {
	return credentialRE.ReplaceAllString(value, "://***@")
}

// Strings returns a copy with embedded URL credentials removed.
func Strings(values []string) []string {
	redacted := make([]string, len(values))
	for i, value := range values {
		redacted[i] = URL(value)
	}
	return redacted
}
