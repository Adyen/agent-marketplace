package diag

import (
	"strings"
	"unicode/utf8"
)

const sanitizedDiagnosticTruncationMark = "…(truncated)"

// SanitizeText makes untrusted diagnostic text safe for structured storage and
// terminal rendering, then bounds it in bytes on a UTF-8 rune boundary. It
// replaces C0 controls, DEL, C1 controls, Unicode line separators, and invalid
// UTF-8 with spaces; bidirectional formatting controls are removed because
// they can reorder surrounding text without adding visible glyphs.
func SanitizeText(s string, limit int) string {
	if limit <= 0 || s == "" {
		return ""
	}
	s = strings.TrimSpace(s)
	if !needsSanitizing(s) {
		return truncateText(s, limit)
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case isBidiControl(r):
			// Drop bidi formatting controls rather than replacing them: the
			// control itself carries no diagnostic information.
		case r == utf8.RuneError, r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f,
			r == '\u2028', r == '\u2029':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return truncateText(b.String(), limit)
}

func needsSanitizing(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < 0x20 || b == 0x7f {
			return true
		}
		if b >= 0x80 {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError || (r >= 0x80 && r <= 0x9f) || isBidiControl(r) || r == '\u2028' || r == '\u2029' {
				return true
			}
			i += size - 1
		}
	}
	return false
}

func isBidiControl(r rune) bool {
	return (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}

func truncateText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit - len(sanitizedDiagnosticTruncationMark)
	if cut <= 0 {
		cut = limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		return s[:cut]
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + sanitizedDiagnosticTruncationMark
}
