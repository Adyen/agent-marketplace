package store

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	dsnURLUserinfoRE    = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s@]+@`)
	dsnQueryParamRE     = regexp.MustCompile(`(?i)([?&])([^=&#\s]+)=([^&#\s]*)`)
	dsnKeyValueSecretRE = regexp.MustCompile(`(?i)\b(password|token|access_token|api_key|apikey|client_secret|secret|auth|authorization)\s*=\s*(?:"[^"]*"|'[^']*'|[^\s&]+)`)
)

// SanitizeDSNError returns an error with DSN credentials removed from its
// message. It intentionally does not wrap the original error: callers must not
// be able to recover a secret-bearing driver error through errors.Unwrap.
// Store startup errors commonly contain the driver-normalized DSN, so
// sanitization is deliberately applied both to the configured DSN and to
// common URL and key/value forms in the driver's message.
func SanitizeDSNError(err error, dsn string) error {
	if err == nil {
		return nil
	}
	message := sanitizeDSNText(err.Error(), dsn)
	return sanitizedError{message: message}
}

type sanitizedError struct {
	message string
}

func (e sanitizedError) Error() string { return e.message }

func isDSNSecretQueryKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "password", "token", "access_token", "api_key", "apikey", "client_secret", "secret", "auth", "authorization":
		return true
	default:
		return false
	}
}

func sanitizeDSNText(message, dsn string) string {
	// Never echo the complete configured DSN. Besides credentials, this keeps
	// future driver error formats from accidentally exposing newly supported
	// secret-bearing DSN parameters.
	if dsn != "" {
		message = strings.ReplaceAll(message, dsn, "[redacted dsn]")
	}
	if u, err := url.Parse(dsn); err == nil && u.User != nil {
		message = strings.ReplaceAll(message, u.User.String(), "[redacted userinfo]")
		if password, ok := u.User.Password(); ok {
			message = strings.ReplaceAll(message, password, "[redacted]")
		}
	}
	message = dsnURLUserinfoRE.ReplaceAllString(message, `${1}[redacted userinfo]@`)
	message = dsnQueryParamRE.ReplaceAllStringFunc(message, func(value string) string {
		parts := dsnQueryParamRE.FindStringSubmatch(value)
		key, err := url.QueryUnescape(parts[2])
		if err != nil || !isDSNSecretQueryKey(key) {
			return value
		}
		return parts[1] + parts[2] + "=[redacted]"
	})
	message = dsnKeyValueSecretRE.ReplaceAllStringFunc(message, func(value string) string {
		eq := strings.IndexByte(value, '=')
		return value[:eq+1] + "[redacted]"
	})
	return message
}
