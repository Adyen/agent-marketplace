package analyze

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxAuditSummaryBytes   = 1024
	MaxAuditErrorBytes     = 512
	MaxFindingMessageBytes = 4096
	MaxSourceFileBytes     = 1024
)

// NormalizeConsentable returns the canonical form of a consentable pointer.
// nil and explicit true both mean "consentable", so they normalize to nil.
// A false pointer is deep-cloned so caller mutation cannot affect the audit.
func NormalizeConsentable(c *bool) *bool {
	if c == nil || *c {
		return nil
	}
	v := false
	return &v
}

// ValidateConsentable rejects an explicit true consentable pointer as
// non-canonical and returns a deep clone of a false pointer.
func ValidateConsentable(c *bool) (*bool, error) {
	if c != nil && *c {
		return nil, fmt.Errorf("explicit true consentable is not canonical")
	}
	return NormalizeConsentable(c), nil
}

// SanitizeString removes invalid UTF-8 byte sequences and control characters,
// then bounds the result to max bytes without splitting a rune. If truncation
// occurs, an ellipsis is appended; for max <= 3 the ellipsis is shortened to fit.
func SanitizeString(s string, max int) string {
	if max <= 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 byte; skip.
			i += size
			continue
		}
		if unicode.IsControl(r) {
			// Control character; strip.
			i += size
			continue
		}
		b.WriteRune(r)
		i += size
	}
	out := b.String()
	if len(out) <= max {
		return out
	}
	if max <= 3 {
		return strings.Repeat(".", max)
	}
	end := max - 3
	for end > 0 && !utf8.RuneStart(out[end]) {
		end--
	}
	if end <= 0 {
		// No full rune fits before the ellipsis budget; return the ellipsis alone.
		return "..."
	}
	return out[:end] + "..."
}

// sanitizeSourceRange sanitizes the file path of a source range.
func sanitizeSourceRange(sr *SourceRange) *SourceRange {
	if sr == nil {
		return nil
	}
	cp := *sr
	cp.File = SanitizeString(cp.File, MaxSourceFileBytes)
	return &cp
}

// sanitizeFinding sanitizes a finding's message, source range file, and
// consentable flag.
func sanitizeFinding(f Finding) Finding {
	f.Message = SanitizeString(f.Message, MaxFindingMessageBytes)
	f.SourceRange = sanitizeSourceRange(f.SourceRange)
	f.Consentable = NormalizeConsentable(f.Consentable)
	return f
}

// sanitizeAudit sanitizes all user-provided strings in an audit before it is
// persisted or served.
func sanitizeAudit(audit Audit) Audit {
	audit.Summary = SanitizeString(audit.Summary, MaxAuditSummaryBytes)
	audit.Error = SanitizeString(audit.Error, MaxAuditErrorBytes)
	for i := range audit.Findings {
		audit.Findings[i] = sanitizeFinding(audit.Findings[i])
	}
	return audit
}
