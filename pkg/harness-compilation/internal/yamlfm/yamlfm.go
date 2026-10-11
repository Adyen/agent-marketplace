// Package yamlfm provides a YAML frontmatter parser for Markdown files
// that use the `---` delimiter convention (as seen in Claude skill files).
package yamlfm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// QuoteScalar returns s formatted as a YAML scalar suitable for inline
// "key: <scalar>" frontmatter. If s can be safely emitted as a plain
// (unquoted) scalar it is returned unchanged; otherwise it is wrapped so the
// value round-trips through a strict YAML parser.
//
// This exists because frontmatter renderers historically interpolated string
// values directly (fmt.Sprintf("%s: %s", key, v)), which produces invalid YAML
// whenever the value contains a colon-space, a leading indicator character, a
// trailing comment marker, or otherwise looks like a non-string scalar. Strict
// parsers (e.g. Devin's skill loader) then reject the whole frontmatter and
// silently skip the resource.
func QuoteScalar(s string) string {
	if !scalarNeedsQuoting(s) {
		return s
	}
	// Values containing control characters / newlines cannot live in a
	// single-line single-quoted scalar; fall back to a double-quoted form
	// (strconv.Quote escapes are valid YAML double-quoted escapes and stay on
	// one line, avoiding YAML line-folding).
	if strings.ContainsAny(s, "\n\r\t\u0085\u2028\u2029") {
		return strconv.Quote(s)
	}
	// Single-quoted scalars carry every other character verbatim; embedded
	// single quotes are escaped by doubling them.
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// scalarNeedsQuoting reports whether s would be misinterpreted (or rejected) by
// a YAML parser when emitted as a bare plain scalar after "key: ".
func scalarNeedsQuoting(s string) bool {
	// Empty string must be quoted so it round-trips as an empty string;
	// an unquoted "key: " parses back as null, losing the string type.
	if s == "" {
		return true
	}
	if strings.ContainsAny(s, "\u0085\u2028\u2029") {
		return true
	}
	// Leading or trailing whitespace is not preserved by a plain scalar.
	if s[0] == ' ' || s[0] == '\t' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t' {
		return true
	}
	// A leading indicator character changes the meaning of a plain scalar.
	switch s[0] {
	case '!', '&', '*', '?', '|', '>', '%', '@', '`', '"', '\'', ',', '[', ']', '{', '}', '#', ':':
		return true
	case '-':
		// "-" alone or "- ..." begins a block sequence entry.
		if len(s) == 1 || s[1] == ' ' {
			return true
		}
	}
	// Plain scalars that a YAML parser would decode as a non-string type must
	// be quoted to remain strings.
	switch strings.ToLower(s) {
	case "null", "~", "true", "false", "yes", "no", "on", "off":
		return true
	}
	if looksNumeric(s) {
		return true
	}
	// Interior structural sequences that break or change a plain scalar.
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n', '\r', '\t':
			return true
		case ':':
			// ": " or a trailing ":" begins a mapping value.
			if i+1 == len(s) || s[i+1] == ' ' {
				return true
			}
		case '#':
			// " #" begins a comment.
			if i > 0 && (s[i-1] == ' ' || s[i-1] == '\t') {
				return true
			}
		}
	}
	return false
}

// looksNumeric reports whether s would be parsed as a YAML integer or float
// (and therefore needs quoting to stay a string).
func looksNumeric(s string) bool {
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	return false
}

// ParseYAMLFrontmatter reads from r, detects `---` frontmatter delimiters,
// parses the YAML between them, and returns the remaining body content.
//
// If no frontmatter delimiters are present, it returns (nil, fullContent, nil).
// The frontmatter map is non-nil and empty when delimiters exist but contain
// no keys.
func ParseYAMLFrontmatter(r io.Reader) (frontmatter map[string]any, body string, error error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, "", fmt.Errorf("yamlfm: read: %w", err)
	}

	content := string(data)

	// Frontmatter must start at the very beginning of the file.
	if !startsWithFrontmatter(content) {
		return nil, content, nil
	}

	// Find the closing delimiter after the opening one.
	openEnd := len("---\n")
	if len(content) > 3 && content[3] == '\r' {
		openEnd = len("---\r\n")
	}

	closeIdx := findClosingDelimiter(content, openEnd)
	if closeIdx < 0 {
		return nil, "", errors.New("yamlfm: frontmatter opening delimiter found but no closing delimiter")
	}

	// Extract YAML content between delimiters.
	yamlContent := content[openEnd:closeIdx]
	// Trim trailing newline before the closing delimiter to keep YAML clean.
	yamlContent = trimTrailingNewline(yamlContent)

	// Body is everything after the closing delimiter and its trailing newline.
	bodyStart := closeIdx + 3 // skip "---"
	if bodyStart < len(content) && content[bodyStart] == '\r' && bodyStart+1 < len(content) && content[bodyStart+1] == '\n' {
		bodyStart += 2
	} else if bodyStart < len(content) && content[bodyStart] == '\n' {
		bodyStart++
	}
	body = content[bodyStart:]

	// Parse YAML.
	var fm map[string]any
	if len(yamlContent) == 0 {
		fm = make(map[string]any)
		return fm, body, nil
	}

	if err := yaml.Unmarshal([]byte(yamlContent), &fm); err != nil {
		return nil, "", fmt.Errorf("yamlfm: yaml parse: %w", err)
	}

	if fm == nil {
		fm = make(map[string]any)
	}

	return fm, body, nil
}

// startsWithFrontmatter returns true if content begins with "---\n" or "---\r\n".
func startsWithFrontmatter(content string) bool {
	return bytes.HasPrefix([]byte(content), []byte("---\n")) ||
		bytes.HasPrefix([]byte(content), []byte("---\r\n"))
}

// findClosingDelimiter finds the next standalone "---" line starting from
// the offset. Returns the index of the start of the closing "---", or -1.
func findClosingDelimiter(content string, from int) int {
	// Search for a line that is exactly "---" (possibly preceded by a newline
	// from the opening delimiter or the last YAML line, and followed by \n or EOF).
	//
	// We scan line-by-line from `from` to handle all cases: consecutive
	// delimiters (empty frontmatter), standard delimiters, and closing
	// delimiter at EOF without trailing newline.
	rem := content[from:]
	for {
		// Find the next "---" occurrence.
		idx := bytes.Index([]byte(rem), []byte("---"))
		if idx < 0 {
			return -1
		}

		absIdx := from + idx

		// Verify that "---" starts on its own line:
		// either at position 0 (from) or preceded by \n.
		if absIdx == from || content[absIdx-1] == '\n' {
			// Verify that the line is exactly "---" (not part of a longer token).
			afterIdx := absIdx + 3
			if afterIdx >= len(content) || content[afterIdx] == '\n' || (content[afterIdx] == '\r' && afterIdx+1 < len(content) && content[afterIdx+1] == '\n') {
				return absIdx
			}
		}

		// Move past this "---" and continue searching.
		rem = rem[idx+3:]
		from = absIdx + 3
	}
}

// trimTrailingNewline removes a single trailing \n or \r\n.
func trimTrailingNewline(s string) string {
	if len(s) >= 2 && s[len(s)-2] == '\r' && s[len(s)-1] == '\n' {
		return s[:len(s)-2]
	}
	if len(s) >= 1 && s[len(s)-1] == '\n' {
		return s[:len(s)-1]
	}
	return s
}
