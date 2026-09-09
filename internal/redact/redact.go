package redact

import "regexp"

var (
	credentialRE    = regexp.MustCompile(`://[^/\s?#]*@`)
	scpCredentialRE = regexp.MustCompile(`^[^@\s/:]+:[^@\s]+@([^\s/:]+:)`)
)

// URL removes embedded URL credentials while preserving the host and path.
func URL(value string) string {
	value = credentialRE.ReplaceAllString(value, "://***@")
	return scpCredentialRE.ReplaceAllString(value, "***@$1")
}

// Strings returns a copy with embedded URL credentials removed.
func Strings(values []string) []string {
	redacted := make([]string, len(values))
	for i, value := range values {
		redacted[i] = URL(value)
	}
	return redacted
}
