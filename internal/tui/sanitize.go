package tui

import (
	"strings"
	"unicode/utf8"
)

type terminalDataMode uint8

const (
	terminalSingleLine terminalDataMode = iota
	terminalMarkdownBody
)

const upperHex = "0123456789ABCDEF"

func sanitizeTerminalData(value string, mode terminalDataMode) string {
	if value == "" || !terminalDataNeedsSanitizing(value, mode) {
		return value
	}
	var sanitized strings.Builder
	sanitized.Grow(len(value))
	for offset := 0; offset < len(value); {
		b := value[offset]
		if b < utf8.RuneSelf {
			if b == '&' && mode == terminalMarkdownBody {
				if consumed, decoded, ok := unsafeMarkdownEntity(value[offset:]); ok {
					writeTerminalRuneEscape(&sanitized, decoded)
					offset += consumed
					continue
				}
			}
			offset++
			if b >= 0x20 && b != 0x7f {
				sanitized.WriteByte(b)
				continue
			}
			if b == '\n' && mode == terminalMarkdownBody {
				sanitized.WriteByte(b)
				continue
			}
			writeByteEscape(&sanitized, b)
			continue
		}

		r, size := utf8.DecodeRuneInString(value[offset:])
		if r == utf8.RuneError && size == 1 {
			writeByteEscape(&sanitized, b)
			offset++
			continue
		}
		offset += size
		switch {
		case r >= 0x80 && r <= 0x9f:
			writeByteEscape(&sanitized, byte(r))
		case isTerminalBidiControl(r) || r == 0x2028 || r == 0x2029:
			writeRuneEscape(&sanitized, r)
		default:
			sanitized.WriteRune(r)
		}
	}
	return sanitized.String()
}

func terminalDataNeedsSanitizing(value string, mode terminalDataMode) bool {
	for offset := 0; offset < len(value); {
		b := value[offset]
		if b < utf8.RuneSelf {
			if b == '&' && mode == terminalMarkdownBody {
				if _, _, ok := unsafeMarkdownEntity(value[offset:]); ok {
					return true
				}
			}
			offset++
			if b < 0x20 || b == 0x7f {
				if b != '\n' || mode != terminalMarkdownBody {
					return true
				}
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(value[offset:])
		if r == utf8.RuneError && size == 1 {
			return true
		}
		if (r >= 0x80 && r <= 0x9f) || isTerminalBidiControl(r) || r == 0x2028 || r == 0x2029 {
			return true
		}
		offset += size
	}
	return false
}

// sanitizeMarkdownForTerminal is the last untrusted-data transform before
// Glamour parses Markdown. Glamour entity-decodes text, code spans, links, and
// sanitized raw HTML while producing ANSI, so raw-byte sanitization alone is
// insufficient. unsafeMarkdownEntity rewrites only references whose decoded
// scalar can control a terminal; benign entities and Markdown syntax remain
// untouched for the renderer.
func sanitizeMarkdownForTerminal(value string) string {
	return sanitizeTerminalData(value, terminalMarkdownBody)
}

func unsafeMarkdownEntity(value string) (consumed int, decoded rune, ok bool) {
	if len(value) < 3 || value[0] != '&' {
		return 0, 0, false
	}
	if value[1] == '#' {
		return unsafeNumericMarkdownEntity(value)
	}

	for _, entity := range []struct {
		name string
		rune rune
	}{
		{name: "&Tab;", rune: '\t'},
		{name: "&NewLine;", rune: '\n'},
		{name: "&lrm;", rune: '\u200e'},
		{name: "&rlm;", rune: '\u200f'},
	} {
		if strings.HasPrefix(value, entity.name) {
			return len(entity.name), entity.rune, true
		}
	}
	return 0, 0, false
}

func unsafeNumericMarkdownEntity(value string) (consumed int, decoded rune, ok bool) {
	base := uint64(10)
	offset := 2
	if offset < len(value) && (value[offset] == 'x' || value[offset] == 'X') {
		base = 16
		offset++
	}
	digitsStart := offset
	var numeric uint64
	overflow := false
	for offset < len(value) {
		digit, valid := entityDigit(value[offset], base)
		if !valid {
			break
		}
		if !overflow {
			if numeric > (^uint64(0)-digit)/base {
				overflow = true
			} else {
				numeric = numeric*base + digit
			}
		}
		offset++
	}
	if offset == digitsStart {
		return 0, 0, false
	}
	if offset < len(value) && value[offset] == ';' {
		offset++
	}

	// Extremely large numeric references can wrap in entity decoders before
	// validity checks. Replace them with a visible replacement escape rather
	// than attempting to reproduce decoder-specific overflow behavior.
	if overflow || numeric > utf8.MaxRune {
		return offset, utf8.RuneError, true
	}
	decoded = rune(numeric)
	if !terminalEntityRuneNeedsSanitizing(decoded) {
		return 0, 0, false
	}
	return offset, decoded, true
}

func entityDigit(value byte, base uint64) (uint64, bool) {
	switch {
	case value >= '0' && value <= '9':
		digit := uint64(value - '0')
		return digit, digit < base
	case base == 16 && value >= 'a' && value <= 'f':
		return uint64(value-'a') + 10, true
	case base == 16 && value >= 'A' && value <= 'F':
		return uint64(value-'A') + 10, true
	default:
		return 0, false
	}
}

func terminalEntityRuneNeedsSanitizing(r rune) bool {
	return r <= 0x1f || r == 0x7f || (r >= 0x80 && r <= 0x9f) ||
		isTerminalBidiControl(r) || r == 0x2028 || r == 0x2029
}

func isTerminalBidiControl(r rune) bool {
	return r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}

// sanitizedExitError wraps an error that crosses the TUI boundary back to
// Cobra. Cobra prints it with plain Error() text on the restored terminal —
// after the alternate screen has been left — so any untrusted path, registry
// URL, or backend message embedded in the error must not carry OSC/CSI
// controls or structural newlines onto that terminal. Unwrap preserves the
// original error identity, so errors.Is/errors.As keep working for callers.
type sanitizedExitError struct {
	err error
}

func (e sanitizedExitError) Error() string {
	return sanitizeTerminalData(e.err.Error(), terminalSingleLine)
}

func (e sanitizedExitError) Unwrap() error { return e.err }

// sanitizeReturnedError prepares an error leaving the TUI for display on the
// restored terminal. Nil stays nil so clean exits are untouched.
func sanitizeReturnedError(err error) error {
	if err == nil {
		return nil
	}
	return sanitizedExitError{err: err}
}

func writeTerminalRuneEscape(builder *strings.Builder, r rune) {
	if r <= 0xff {
		writeByteEscape(builder, byte(r))
		return
	}
	writeRuneEscape(builder, r)
}

func writeByteEscape(builder *strings.Builder, b byte) {
	builder.WriteString(`\x`)
	builder.WriteByte(upperHex[b>>4])
	builder.WriteByte(upperHex[b&0x0f])
}

func writeRuneEscape(builder *strings.Builder, r rune) {
	if r <= 0xffff {
		builder.WriteString(`\u`)
		for shift := 12; shift >= 0; shift -= 4 {
			builder.WriteByte(upperHex[byte(r>>shift)&0x0f])
		}
		return
	}
	builder.WriteString(`\U`)
	for shift := 28; shift >= 0; shift -= 4 {
		builder.WriteByte(upperHex[byte(r>>shift)&0x0f])
	}
}
