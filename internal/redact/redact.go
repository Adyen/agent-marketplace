package redact

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const redactedURL = "[redacted URL]"

var (
	credentialRE    = regexp.MustCompile(`://[^/\s?#]*@`)
	scpCredentialRE = regexp.MustCompile(`^[^@\s/:]+:[^@\s/][^@\s]*@([^\s/:]+:)`)
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

// URLValue redacts a value that is exactly one URL. Userinfo, query, and
// fragment are replaced even when they contain whitespace, and a value that
// does not parse as a hierarchical URL fails closed to a fixed marker.
func URLValue(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Opaque != "" {
		return redactedURL
	}
	redacted := (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawPath: u.RawPath}).String()
	if u.User != nil {
		redacted = strings.Replace(redacted, "//", "//***@", 1)
	}
	switch {
	case u.RawQuery != "" || u.ForceQuery:
		redacted += "?***"
	case u.Fragment != "":
		redacted += "#***"
	}
	return redacted
}

// Error returns err with embedded URL credentials removed from its message.
// The original error remains available to errors.Is and errors.As.
func Error(err error) error {
	if err == nil {
		return nil
	}
	return redactedError{err: err}
}

type redactedError struct{ err error }

func (e redactedError) Error() string {
	message := e.err.Error()
	var urlErr *url.Error
	if !errors.As(e.err, &urlErr) {
		return URL(message)
	}
	const placeholder = "\x00url\x00"
	message = URL(strings.ReplaceAll(message, strconv.Quote(urlErr.URL), placeholder))
	return strings.ReplaceAll(message, placeholder, strconv.Quote(URLValue(urlErr.URL)))
}

func (e redactedError) Unwrap() error { return e.err }

// Strings returns a copy with embedded URL credentials removed.
func Strings(values []string) []string {
	redacted := make([]string, len(values))
	for i, value := range values {
		redacted[i] = URL(value)
	}
	return redacted
}
