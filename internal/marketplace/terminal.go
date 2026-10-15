package marketplace

import (
	"fmt"
	"strconv"
	"strings"
)

// QuoteTerminal returns an ASCII-only, quoted representation suitable for
// terminal output. QuoteToASCII escapes controls, ANSI bytes, and bidi/non-ASCII
// runes instead of allowing resource metadata to alter terminal presentation.
func QuoteTerminal(value string) string {
	return strconv.QuoteToASCII(value)
}

// TerminalString keeps JSON preview fields terminal-safe while retaining JSON
// string escaping (including the conventional \n and \uXXXX escapes).
type TerminalString string

func (s TerminalString) MarshalJSON() ([]byte, error) {
	var quoted strings.Builder
	quoted.WriteByte('"')
	for _, r := range string(s) {
		switch {
		case r == '"' || r == '\\':
			quoted.WriteByte('\\')
			quoted.WriteRune(r)
		case r == '\b':
			quoted.WriteString("\\b")
		case r == '\f':
			quoted.WriteString("\\f")
		case r == '\n':
			quoted.WriteString("\\n")
		case r == '\r':
			quoted.WriteString("\\r")
		case r == '\t':
			quoted.WriteString("\\t")
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&quoted, "\\u%04x", r)
		case r < 0x80:
			quoted.WriteRune(r)
		case r <= 0xffff:
			fmt.Fprintf(&quoted, "\\u%04x", r)
		default:
			r -= 0x10000
			fmt.Fprintf(&quoted, "\\u%04x\\u%04x", 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		}
	}
	quoted.WriteByte('"')
	return []byte(quoted.String()), nil
}

func TerminalStrings(values []string) []TerminalString {
	result := make([]TerminalString, len(values))
	for i, value := range values {
		result[i] = TerminalString(value)
	}
	return result
}
