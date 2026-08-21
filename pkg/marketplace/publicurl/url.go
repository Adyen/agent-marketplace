// Package publicurl validates and sanitizes URLs that cross the Marketplace
// public metadata boundary.
package publicurl

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var errInvalid = errors.New("must be an HTTP(S) URL with a host")

// Validate checks a URL which is safe to publish as public metadata. Public
// URLs must not carry authentication in userinfo or query parameters, and may
// not contain fragments (which are client-side data, not server endpoints).
func Validate(raw string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return errInvalid
	}
	u, err := url.Parse(value)
	if err != nil || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) || u.Hostname() == "" {
		return errInvalid
	}
	if u.User != nil {
		return errors.New("must not contain URL userinfo")
	}
	if strings.Contains(value, "#") || u.Fragment != "" || u.RawFragment != "" {
		return errors.New("must not contain a URL fragment")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return errors.New("must contain a valid URL query")
	}
	for key := range query {
		if IsSecretQueryKey(key) {
			return errors.New("must not contain credentials in URL query parameters")
		}
	}
	return nil
}

// IsSecretQueryKey reports whether a decoded query key looks like a
// credential, signing parameter, or cloud-provider secret. It deliberately
// errs on the side of rejecting ambiguous names at a public metadata boundary.
func IsSecretQueryKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	normalized := strings.NewReplacer("-", "", "_", "", ".", "").Replace(key)
	if normalized == "" {
		return false
	}
	for _, part := range []string{
		"token", "password", "passwd", "credential", "auth", "authorization",
		"signature", "sig", "secret",
	} {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	if normalized == "key" || strings.Contains(normalized, "apikey") ||
		strings.Contains(normalized, "accesskey") || strings.Contains(normalized, "privatekey") ||
		strings.Contains(normalized, "sessionkey") {
		return true
	}
	// AWS query authentication uses names such as X-Amz-Security-Token and
	// X-Amz-Signature; retain an explicit cloud-provider guard for new variants.
	return (strings.HasPrefix(normalized, "aws") || strings.HasPrefix(normalized, "xamz")) &&
		(strings.Contains(normalized, "key") || strings.Contains(normalized, "token") ||
			strings.Contains(normalized, "signature") || strings.Contains(normalized, "credential"))
}

// Sanitize returns a safe public representation of a legacy value. Invalid
// syntax or a non-HTTP(S) value is omitted. Userinfo, fragments, and unsafe
// query values are removed rather than returned.
func Sanitize(raw string) string {
	return sanitize(raw, false)
}

// Redact is intended for legacy source-origin arrays. It retains a parseable
// HTTP(S) origin while removing sensitive components; malformed and unsupported
// legacy values fail closed with a fixed marker.
func Redact(raw string) string {
	if sanitized := sanitize(raw, true); sanitized != "" {
		return sanitized
	}
	return "[redacted origin]"
}

func sanitize(raw string, redactQuerySecrets bool) string {
	value := strings.TrimSpace(raw)
	u, err := url.Parse(value)
	if err != nil || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) || u.Hostname() == "" {
		return ""
	}
	u.User = nil
	u.Fragment = ""
	u.RawFragment = ""
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return ""
	}
	for key := range query {
		if IsSecretQueryKey(key) {
			if redactQuerySecrets {
				query.Set(key, "[redacted]")
			} else {
				delete(query, key)
			}
		}
	}
	u.RawQuery = query.Encode()
	return u.String()
}

// ErrorForField decorates a policy error without including the potentially
// sensitive URL value in the resulting configuration error.
func ErrorForField(field, raw string) error {
	if err := Validate(raw); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}
