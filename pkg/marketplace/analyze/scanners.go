package analyze

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	htmlCommentRe = regexp.MustCompile(`(?s:<!--.*?-->)`)

	suspiciousCallRe = regexp.MustCompile(`(?i)(?:\b(?:setTimeout|setInterval|exec)\s*\(|\bnew\s+Function\s*\(|\bwindow\s*\[\s*["']eval["']\s*\])`)

	evalSystemRe = regexp.MustCompile(`(?i)\b(?:eval|system)` +
		`(?:\s|//[^\n]*|/\*[\s\S]*?\*/|\p{Z}|\x{FEFF})*` +
		`(?:\s*\.\s*call\s*)?` +
		`\s*\(`)

	wordRe = regexp.MustCompile(`\pL+`)

	hexLikeRe = regexp.MustCompile(`(?i)0x(?:[0-9a-fA-F]{2}){2,}`)
)

// suspiciousTokenRe matches decoded strings that are likely hostile.
var suspiciousTokens = []string{
	"eval", "system", "exec", "script", "javascript", "document", "window",
	"fetch", "import", "require", "shell", "bash", "sh -c", "curl", "wget",
	"rm -rf", "<script", "onerror", "onload", "alert", "prompt", "console",
	"function", "fromcharcode", "atob", "unescape", "decodeuri", "filesystem",
	"cmd", "powershell", "python", "perl", "ruby", "settimeout", "setinterval",
}

// runSharedScanners runs all content scanners on a single sample.
func runSharedScanners(content, file, analyzer string) []Finding {
	var findings []Finding
	findings = append(findings, ScanHTMLComments(content, file, analyzer)...)
	findings = append(findings, ScanZeroWidth(content, file, analyzer)...)
	findings = append(findings, ScanUnsafeBidi(content, file, analyzer)...)
	findings = append(findings, ScanTerminalControl(content, file, analyzer)...)
	findings = append(findings, ScanDangerousURLs(content, file, analyzer)...)
	findings = append(findings, ScanRemoteReferences(content, file, analyzer)...)
	findings = append(findings, ScanSuspiciousPaths(content, file, analyzer)...)
	findings = append(findings, ScanObfuscatedInstructions(content, file, analyzer)...)
	return findings
}

// ScanHTMLComments finds HTML-style comments.
func ScanHTMLComments(content, file, analyzer string) []Finding {
	var findings []Finding
	for _, loc := range htmlCommentRe.FindAllStringIndex(content, -1) {
		msg := fmt.Sprintf("HTML comment (%s)", describeBytes(loc[1]-loc[0]))
		findings = append(findings, NewFinding(FindingHTMLComment, msg, analyzer, SourceRangeAt(file, content, loc[0])))
	}
	return findings
}

// isZeroWidth reports whether r is an invisible or zero-width character.
func isZeroWidth(r rune) bool {
	switch r {
	case '\u00AD', '\u034F', '\u180E', '\u200B', '\u200C', '\u200D',
		'\u200E', '\u200F', '\u2060', '\u2061', '\u2062', '\u2063', '\u2064',
		'\u206A', '\u206B', '\u206C', '\u206D', '\u206E', '\u206F', '\uFEFF',
		'\uFE00', '\uFE01', '\uFE02', '\uFE03', '\uFE04', '\uFE05', '\uFE06',
		'\uFE07', '\uFE08', '\uFE09', '\uFE0A', '\uFE0B', '\uFE0C', '\uFE0D',
		'\uFE0E', '\uFE0F':
		return true
	}
	return false
}

// ScanZeroWidth finds zero-width / invisible Unicode characters.
func ScanZeroWidth(content, file, analyzer string) []Finding {
	var findings []Finding
	start := -1
	runeCount := 0
	for i, r := range content {
		if isZeroWidth(r) {
			if start == -1 {
				start = i
			}
			runeCount++
			continue
		}
		if start != -1 {
			msg := fmt.Sprintf("zero-width/invisible characters (%d runes)", runeCount)
			findings = append(findings, NewFinding(FindingZeroWidth, msg, analyzer, SourceRangeAt(file, content, start)))
			start = -1
			runeCount = 0
		}
	}
	if start != -1 {
		msg := fmt.Sprintf("zero-width/invisible characters (%d runes)", runeCount)
		findings = append(findings, NewFinding(FindingZeroWidth, msg, analyzer, SourceRangeAt(file, content, start)))
	}
	return findings
}

// isUnsafeBidi reports whether r is a bidirectional override character.
func isUnsafeBidi(r rune) bool {
	switch r {
	case '\u202A', '\u202B', '\u202C', '\u202D', '\u202E',
		'\u2066', '\u2067', '\u2068', '\u2069',
		'\u061C':
		return true
	}
	return false
}

// ScanUnsafeBidi finds bidirectional override characters.
func ScanUnsafeBidi(content, file, analyzer string) []Finding {
	var findings []Finding
	start := -1
	runeCount := 0
	for i, r := range content {
		if isUnsafeBidi(r) {
			if start == -1 {
				start = i
			}
			runeCount++
			continue
		}
		if start != -1 {
			msg := fmt.Sprintf("bidirectional override characters (%d runes)", runeCount)
			findings = append(findings, NewFinding(FindingUnsafeBidi, msg, analyzer, SourceRangeAt(file, content, start)))
			start = -1
			runeCount = 0
		}
	}
	if start != -1 {
		msg := fmt.Sprintf("bidirectional override characters (%d runes)", runeCount)
		findings = append(findings, NewFinding(FindingUnsafeBidi, msg, analyzer, SourceRangeAt(file, content, start)))
	}
	return findings
}

// ScanTerminalControl finds C0 control bytes (except tab/newline/cr), DEL,
// C1 control characters, and ANSI/CSI/OSC escape sequences.
func ScanTerminalControl(content, file, analyzer string) []Finding {
	var findings []Finding
	start := -1
	var kind byte

	for i := 0; i < len(content); {
		b := content[i]
		if b == 0x1B {
			seqEnd, seqKind := consumeEscapeSequence(content, i)
			if seqEnd > i {
				if start == -1 {
					start = i
					kind = seqKind
				}
				i = seqEnd
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(content[i:])
		isCtrl := isTerminalControlRune(r, content, i, size)
		if isCtrl {
			if start == -1 {
				start = i
				kind = 'C'
			}
			i += size
			continue
		}

		if start != -1 {
			msg := terminalControlMessage(content, start, i, kind)
			findings = append(findings, NewFinding(FindingTerminalControl, msg, analyzer, SourceRangeAt(file, content, start)))
			start = -1
			kind = 0
		}
		i += size
	}

	if start != -1 {
		msg := terminalControlMessage(content, start, len(content), kind)
		findings = append(findings, NewFinding(FindingTerminalControl, msg, analyzer, SourceRangeAt(file, content, start)))
	}
	return findings
}

func isTerminalControlRune(r rune, content string, i, size int) bool {
	if r == utf8.RuneError && size == 1 {
		// Invalid UTF-8 byte; treat raw high control bytes (0x80-0x9F) as control.
		b := content[i]
		return b >= 0x80 && b <= 0x9F
	}
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	if r <= 0x1F || r == 0x7F || (r >= 0x80 && r <= 0x9F) {
		return true
	}
	return false
}

// consumeEscapeSequence attempts to consume an ANSI/CSI/OSC sequence starting
// at offset i. It returns the exclusive end index and a kind byte, or (i, 0).
func consumeEscapeSequence(s string, i int) (int, byte) {
	if i >= len(s) || s[i] != 0x1B {
		return i, 0
	}
	if i+1 >= len(s) {
		return i, 0
	}
	next := s[i+1]
	switch next {
	case '[':
		// CSI: parameters 0x30-0x3F, intermediate 0x20-0x2F, final 0x40-0x7E.
		j := i + 2
		for j < len(s) {
			c := s[j]
			if c >= 0x40 && c <= 0x7E {
				return j + 1, 'S'
			}
			if (c >= 0x30 && c <= 0x3F) || (c >= 0x20 && c <= 0x2F) {
				j++
				continue
			}
			break
		}
	case ']':
		// OSC: terminated by BEL (0x07) or ST (ESC \).
		j := i + 2
		for j < len(s) {
			if s[j] == 0x07 {
				return j + 1, 'O'
			}
			if s[j] == 0x1B && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2, 'O'
			}
			j++
		}
	case '(', ')', '#', '=', '>', '<', 'N', 'O', 'P', 'V', 'W', 'X', 'Y', 'Z', '6', '7', '8', '9':
		// Single-letter or two-letter sequences.
		if i+2 < len(s) {
			return i + 3, 'S'
		}
		return i + 2, 'S'
	}
	return i, 0
}

func terminalControlMessage(content string, start, end int, kind byte) string {
	prefix := "control character"
	switch kind {
	case 'S':
		prefix = "ANSI/CSI escape sequence"
	case 'O':
		prefix = "OSC escape sequence"
	}
	if end-start > 16 {
		return fmt.Sprintf("%s (%s starting with %q)", prefix, describeBytes(end-start), truncate(strconv.Quote(content[start:start+16]), 80))
	}
	return fmt.Sprintf("%s (%s: %q)", prefix, describeBytes(end-start), truncate(strconv.Quote(content[start:end]), 80))
}

// Character classes shared by the dangerous-URL patterns.
const (
	// dataURLMediaChar is the RFC 2045 media-type and parameter character
	// set. Whitespace is deliberately absent, so a media type can never span
	// the words of a sentence.
	dataURLMediaChar = `A-Za-z0-9!#$&^_.+*`

	// urlStrippedGap are the characters a browser removes from anywhere in a
	// URL (ASCII tab, LF, CR), so "file:<TAB>//x" is the same URL as
	// "file://x". A space is not in this set: in a path it survives as %20 and
	// addresses something else entirely, which is exactly why "file:
	// config.yaml" and "Attach the file: report.pdf" are prose, not URLs.
	urlStrippedGap = `\t\n\r`
)

// Every pattern is anchored at the byte after the scheme colon, so the
// scanner walks the colons in a sample (a memchr scan) instead of running an
// unanchored automaton over the whole sample for every scheme.
var (
	// scriptURLBodyRe requires a code body after javascript:/vbscript:.
	// Whitespace before the body is allowed because the body is source code,
	// where leading whitespace is insignificant and browsers strip tab/LF/CR
	// from URLs anyway; but the first code byte must be one that can legally
	// begin a script, which markdown decoration ("**JavaScript:** Node 18+")
	// and closing punctuation cannot.
	scriptURLBodyRe = regexp.MustCompile(`\A\s*[^\s*#|>=?@)\]},.:^]`)

	// dataURLBodyRe requires an RFC 2397 body,
	// data:[<mediatype>][;base64],<data>. Three shapes qualify: a ";base64"
	// payload marker, a complete body ending at the mandatory comma, or a
	// "type/subtype" media type. The whole-body shapes tolerate leading
	// whitespace because the WHATWG data-URL processor strips whitespace
	// around the media type, so "data: text/html,<script>" really does
	// render; the comma-less media type does not, so "Input data: CSV/rows"
	// stays clean. Either way a bare "data:" followed by an English word is
	// not a data URI.
	dataURLBodyRe = regexp.MustCompile(`(?i)\A(?:` +
		`\s*[` + dataURLMediaChar + `/;=-]*;\s*base64` +
		`|\s*(?:[` + dataURLMediaChar + `-]+/[` + dataURLMediaChar + `-]+)?(?:;[` + dataURLMediaChar + `=-]*)*,` +
		`|[` + urlStrippedGap + `]*[` + dataURLMediaChar + `-]+/[` + dataURLMediaChar + `-]+)`)

	// fileURLBodyRe requires a path component: "file:/" (canonically
	// "file://"), a backslash (browsers treat "\" as "/" for special schemes)
	// or a Windows drive letter ("file:C:/"). Prose such as "Attach the file:
	// report.pdf" has no path component.
	fileURLBodyRe = regexp.MustCompile(`(?i)\A[` + urlStrippedGap + `]*(?:[\\/]|[a-z]:[\\/])`)
)

// dangerousURLScheme couples a scheme name with the pattern its body must
// match before the "<name>:" token counts as a URL of that scheme rather than
// an English word followed by a colon.
type dangerousURLScheme struct {
	name string
	body *regexp.Regexp
}

var dangerousURLSchemes = [...]dangerousURLScheme{
	{"javascript", scriptURLBodyRe},
	{"vbscript", scriptURLBodyRe},
	{"data", dataURLBodyRe},
	{"file", fileURLBodyRe},
}

// ScanDangerousURLs finds URL schemes that can execute code or access local
// files. It requires evidence that the token really is a URL of that scheme:
// "data" and "file" are ordinary English words that documentation constantly
// follows with a colon ("**With data:** User uploads a CSV", "file:
// config.yaml"), and reporting those rejected whole catalogs. Encoded and
// whitespace-padded variants are still caught, because every pattern is also
// applied to the URL-decoded view of each token.
func ScanDangerousURLs(content, file, analyzer string) []Finding {
	var findings []Finding
	ctx := &urlScanContext{findings: &findings, content: content, file: file, analyzer: analyzer}

	ctx.scanString(content, 0, nil)

	// Encoded variants ("javascript%3Aalert(1)", "data&#x3a;text/html,x")
	// only become visible after decoding; the plain forms are already covered
	// by the pass above, so only tokens carrying an encoding marker are
	// decoded.
	if containsEncodedMarker(content) {
		for _, tok := range tokenizeURLLike(content) {
			if !containsEncodedMarker(tok.text) {
				continue
			}
			decoded, offsets, changed := decodeURLToken(tok.text)
			if !changed {
				continue
			}
			ctx.scanString(decoded, tok.start, offsets)
		}
	}

	return findings
}

// urlScanContext carries the state shared by every dangerous-URL pattern pass
// over one resource sample.
type urlScanContext struct {
	findings *[]Finding
	seen     map[int]bool
	content  string
	file     string
	analyzer string
}

// scanString reports every dangerous URL in s. It walks the colons in s and,
// for each one preceded by a scheme name at a token boundary, requires that
// scheme's body pattern to match. When offsets is non-nil, s is a decoded view
// of content starting at base and offsets maps decoded byte positions back to
// original ones.
func (c *urlScanContext) scanString(s string, base int, offsets []int) {
	for i := 0; i < len(s); {
		idx := strings.IndexByte(s[i:], ':')
		if idx < 0 {
			return
		}
		colon := i + idx
		i = colon + 1
		// Fast reject: every dangerous scheme name ends in 't', 'a' or 'e',
		// so most prose colons ("Steps:", "description:") cost one compare.
		if colon == 0 || !isSchemeTailByte(s[colon-1]) {
			continue
		}

		for _, sch := range dangerousURLSchemes {
			schemeStart := colon - len(sch.name)
			if schemeStart < 0 || !strings.EqualFold(s[schemeStart:colon], sch.name) {
				continue
			}
			// A scheme must start at a token boundary, so "metadata:" and
			// "profile:" are not "data:" and "file:".
			if schemeStart > 0 && isASCIIAlnum(s[schemeStart-1]) {
				continue
			}
			loc := sch.body.FindStringIndex(s[i:])
			if loc == nil {
				continue
			}
			start := schemeStart
			if offsets != nil {
				if schemeStart >= len(offsets) {
					continue
				}
				start = base + offsets[schemeStart]
			}
			c.add(start, sch.name, s[schemeStart:i+loc[1]])
			break
		}
	}
}

// isSchemeTailByte reports whether b is the last letter of a dangerous scheme
// name ("javascripT", "vbscripT", "datA", "filE"), ignoring case.
func isSchemeTailByte(b byte) bool {
	switch b {
	case 't', 'T', 'a', 'A', 'e', 'E':
		return true
	}
	return false
}

// add records one dangerous URL at the given offset in the original content.
// evidence is the matched scheme plus the body that proved it is a URL, taken
// from the decoded view.
func (c *urlScanContext) add(start int, scheme, evidence string) bool {
	if start < 0 || start >= len(c.content) || c.seen[start] {
		return false
	}
	slog.Debug("dangerous url scheme detected",
		"file", c.file, "scheme", scheme, "offset", start, "match", truncate(evidence, 80))
	*c.findings = append(*c.findings, NewFinding(FindingDangerousURL,
		fmt.Sprintf("dangerous URL scheme: %s:", scheme),
		c.analyzer, SourceRangeAt(c.file, c.content, start)))
	if c.seen == nil {
		c.seen = make(map[int]bool)
	}
	c.seen[start] = true
	return true
}

// ScanRemoteReferences finds http://, https://, ftp:// and protocol-relative // URLs.
func ScanRemoteReferences(content, file, analyzer string) []Finding {
	var findings []Finding

	schemeRe := regexp.MustCompile(`(?i)(?:https?|ftp)://`)
	for _, loc := range schemeRe.FindAllStringIndex(content, -1) {
		msg := "remote reference: " + truncate(content[loc[0]:loc[1]], 80)
		findings = append(findings, NewFinding(FindingRemoteReference, msg, analyzer, SourceRangeAt(file, content, loc[0])))
	}

	for i := 0; i+1 < len(content); {
		idx := strings.Index(content[i:], "//")
		if idx == -1 {
			break
		}
		idx += i
		// Skip the :// inside scheme URLs (already handled above) and /// noise.
		if idx > 0 && content[idx-1] == ':' {
			i = idx + 2
			continue
		}
		next := idx + 2
		if next < len(content) && (content[next] == '/' || isSpace(content[next])) {
			i = idx + 2
			continue
		}
		msg := "remote reference: protocol-relative URL"
		findings = append(findings, NewFinding(FindingRemoteReference, msg, analyzer, SourceRangeAt(file, content, idx)))
		i = idx + 2
	}
	return findings
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// pathScanMode selects the precision profile used by the suspicious-path
// scanner. The two call sites have different threat models, so they must not
// share one set of patterns.
type pathScanMode uint8

const (
	// pathScanContent scans resource prose (markdown bodies, descriptions,
	// frontmatter values). Slash-command invocations such as
	// "/analyze-cohorts" and prose ellipses such as "..." are legitimate
	// there, so only sensitive filesystem roots and real traversal segments
	// are reported.
	pathScanContent pathScanMode = iota
	// pathScanFilePath scans FileSet entry paths, where any absolute path or
	// ".." segment is a packaging (zip-slip / path-escape) signal and stays
	// strict.
	pathScanFilePath
)

func (m pathScanMode) String() string {
	if m == pathScanFilePath {
		return "file-path"
	}
	return "content"
}

// ScanSuspiciousPaths finds path traversal and sensitive filesystem paths in
// resource content (prose, markdown bodies, metadata values). It uses the
// content precision profile: a bare "/slash-command" or a prose ellipsis is
// not a filesystem path. Use ScanSuspiciousFilePaths for FileSet entry paths.
func ScanSuspiciousPaths(content, file, analyzer string) []Finding {
	return scanSuspiciousPaths(content, file, analyzer, pathScanContent)
}

// ScanSuspiciousFilePaths finds path traversal and absolute or sensitive
// filesystem paths in a file path taken from a resource FileSet. Archive
// entries must be relative and contained, so any absolute path or ".."
// segment is reported.
func ScanSuspiciousFilePaths(filePath, file, analyzer string) []Finding {
	return scanSuspiciousPaths(filePath, file, analyzer, pathScanFilePath)
}

func scanSuspiciousPaths(content, file, analyzer string, mode pathScanMode) []Finding {
	var findings []Finding
	ctx := &pathScanContext{findings: &findings, content: content, file: file, analyzer: analyzer, mode: mode}

	ctx.scanString(content, 0, nil)

	if containsEncodedMarker(content) {
		for _, tok := range tokenizeURLLike(content) {
			if !containsEncodedMarker(tok.text) {
				continue
			}
			decoded, offsets, changed := decodeURLToken(tok.text)
			if !changed {
				continue
			}
			ctx.scanString(decoded, tok.start, offsets)
		}
	}

	return findings
}

// containsEncodedMarker reports whether s carries any marker of an encoding
// that decodeURLToken can undo: a percent escape, an HTML entity or a
// backslash-hex escape. It is the shared prefilter for both decoded passes
// (dangerous URLs and suspicious paths): without a marker, decoding cannot
// reveal anything the raw pass has not already seen.
func containsEncodedMarker(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '%':
			if i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
				return true
			}
		case '&':
			return true
		case '\\':
			if i+3 < len(s) && s[i+1] == 'x' && isHex(s[i+2]) && isHex(s[i+3]) {
				return true
			}
		}
	}
	return false
}

// Character classes shared by the suspicious-path patterns. A path reference
// in prose is delimited by whitespace, quoting or bracketing; anything else
// (letters, digits, '-', '_', '.') means the token is not a path boundary.
const (
	pathBoundaryLeft  = `\s"'` + "`" + `(<\[\{=,;:|`
	pathBoundaryRight = `\s"'` + "`" + `)>\]\}=,;:|!?`
	pathBodyStop      = `\s"'` + "`" + `<>|)\]\}`
)

var (
	// sensitivePathRe matches unambiguous credential targets anywhere, encoded
	// or not. Unchanged in intent from the original, with the '.' escaped so
	// "~/xssh" no longer matches.
	sensitivePathRe = regexp.MustCompile(`(?i)(/etc/passwd|/etc/shadow|~/\.ssh)`)

	// sensitiveRootPathRe (content mode) matches an absolute path rooted at a
	// sensitive system directory, bounded on both sides. The boundaries keep
	// slash-command invocations ("/deploy", "/dev-setup") and words such as
	// "/etcetera" out, which is the false-positive class that rejected whole
	// catalogs.
	sensitiveRootPathRe = regexp.MustCompile(`(?i)(?:^|[` + pathBoundaryLeft + `])` +
		`(/(?:etc|var|root|proc|sys|dev|boot|usr|bin|sbin|home)(?:/[^` + pathBodyStop + `]*)?)` +
		`(?:$|[` + pathBoundaryRight + `])`)

	// absPathRe (file-path mode) keeps the original strict behaviour: any
	// absolute path segment in an archive entry is a path-escape signal.
	absPathRe = regexp.MustCompile(`([\s"'(<\[\{=]|^)(/(?:[^/\s]+)(?:/[^/\s]+)*)`)

	// pathTraversalSepRe matches a dot run immediately followed by a path
	// separator: "../", "..\", plus the naive-sanitizer bypass forms ".../"
	// and "....//". A dot run without a separator is an ellipsis, not
	// traversal.
	pathTraversalSepRe = regexp.MustCompile(`\.{2,}[\\/]`)

	// pathTraversalTailRe matches a trailing "/.." or "\.." segment (group 1),
	// e.g. "cd /..". The trailing class excludes '.' so Go's "./..." package
	// wildcard is not traversal, and excludes separators because "/../" is
	// already reported by pathTraversalSepRe.
	pathTraversalTailRe = regexp.MustCompile(`([\\/]\.\.)(?:$|[^.\\/A-Za-z0-9_-])`)

	// pathTraversalBareRe matches a standalone ".." token (group 1). Exactly
	// two dots, so a prose ellipsis never matches, and delimited, so a git
	// range such as "main..HEAD" does not either.
	pathTraversalBareRe = regexp.MustCompile(`(?:^|[` + pathBoundaryLeft + `])(\.\.)(?:$|[` + pathBoundaryRight + `])`)

	// tildePathRe matches a home-directory reference.
	tildePathRe = regexp.MustCompile(`~[\\/]`)

	// drivePathRe matches a Windows drive-letter path, not a URL scheme.
	drivePathRe = regexp.MustCompile(`(?i)(?:^|[^A-Za-z])([a-z]:[\\/])`)

	// uncPathRe (file-path mode) matches any double backslash.
	uncPathRe = regexp.MustCompile(`\\\\`)

	// uncContentPathRe (content mode) additionally requires a host segment
	// terminated by a separator, so an escaped backslash inside a prose code
	// sample ("\\d", "\\n") is not reported as a UNC path.
	uncContentPathRe = regexp.MustCompile(`\\\\[^\\/\s]+[\\/]`)
)

// pathScanContext carries the state shared by every suspicious-path pattern
// pass over one resource sample.
type pathScanContext struct {
	findings *[]Finding
	seen     map[int]bool
	content  string
	file     string
	analyzer string
	mode     pathScanMode
}

// scanString runs every pattern for the configured mode over s. When offsets
// is non-nil, s is a decoded view of content starting at base and offsets maps
// decoded byte positions back to original ones.
func (c *pathScanContext) scanString(s string, base int, offsets []int) {
	c.run(sensitivePathRe, 0, s, base, offsets)

	if c.mode == pathScanFilePath {
		c.run(absPathRe, 2, s, base, offsets)
	} else {
		c.run(sensitiveRootPathRe, 1, s, base, offsets)
	}

	c.run(pathTraversalSepRe, 0, s, base, offsets)
	c.run(pathTraversalTailRe, 1, s, base, offsets)
	c.run(pathTraversalBareRe, 1, s, base, offsets)

	c.run(tildePathRe, 0, s, base, offsets)
	c.run(drivePathRe, 1, s, base, offsets)

	if c.mode == pathScanFilePath {
		c.run(uncPathRe, 0, s, base, offsets)
	} else {
		c.run(uncContentPathRe, 0, s, base, offsets)
	}
}

// run reports every match of re in s. group selects the capturing group that
// delimits the reported path, or 0 for the whole match.
func (c *pathScanContext) run(re *regexp.Regexp, group int, s string, base int, offsets []int) {
	if group > 0 {
		for _, loc := range re.FindAllStringSubmatchIndex(s, -1) {
			if len(loc) <= group*2+1 || loc[group*2] < 0 {
				continue
			}
			c.addMatch(s, base, offsets, loc[group*2], loc[group*2+1])
		}
		return
	}
	for _, loc := range re.FindAllStringIndex(s, -1) {
		c.addMatch(s, base, offsets, loc[0], loc[1])
	}
}

// addMatch records a match located at [start,end) inside s, mapping decoded
// offsets back to the original content when needed.
func (c *pathScanContext) addMatch(s string, base int, offsets []int, start, end int) bool {
	if offsets == nil {
		return c.add(start, end)
	}
	return c.addDecoded(s, base, offsets, start, end)
}

func (c *pathScanContext) addDecoded(s string, base int, offsets []int, decStart, decEnd int) bool {
	if decStart < 0 || decEnd > len(s) || offsets == nil {
		return false
	}
	start := base + offsets[decStart]
	end := base + offsets[len(offsets)-1]
	if decEnd < len(offsets) {
		end = base + offsets[decEnd]
	}
	return c.add(start, end)
}

func (c *pathScanContext) add(start, end int) bool {
	if start < 0 || end > len(c.content) || c.seen[start] {
		return false
	}
	match := truncate(c.content[start:end], 80)
	slog.Debug("suspicious path detected",
		"file", c.file, "mode", c.mode.String(), "offset", start, "match", match)
	*c.findings = append(*c.findings, NewFinding(FindingSuspiciousPath,
		fmt.Sprintf("suspicious path: %q", match),
		c.analyzer, SourceRangeAt(c.file, c.content, start)))
	if c.seen == nil {
		c.seen = make(map[int]bool)
	}
	c.seen[start] = true
	return true
}

// ScanObfuscatedInstructions finds encoded or obfuscated hostile instructions.
func ScanObfuscatedInstructions(content, file, analyzer string) []Finding {
	var findings []Finding
	seen := make(map[int]bool)

	// Raw dangerous calls.
	for _, re := range []*regexp.Regexp{evalSystemRe, suspiciousCallRe} {
		for _, loc := range re.FindAllStringIndex(content, -1) {
			if seen[loc[0]] {
				continue
			}
			seen[loc[0]] = true
			findings = append(findings, NewFinding(FindingObfuscatedInstruction,
				fmt.Sprintf("obfuscated instruction: %q", truncate(content[loc[0]:loc[1]], 40)),
				analyzer, SourceRangeAt(file, content, loc[0])))
		}
	}

	// Encoded candidate tokens.
	for _, tok := range tokenizeURLLike(content) {
		decoded, offsets, changed := decodeObfuscatedToken(tok.text)
		if !changed {
			continue
		}
		origStart, ok := firstSuspiciousInstructionOffset(decoded, offsets, tok.start)
		if !ok || seen[origStart] {
			continue
		}
		seen[origStart] = true
		findings = append(findings, NewFinding(FindingObfuscatedInstruction,
			fmt.Sprintf("obfuscated instruction: %q", truncate(decoded, 60)),
			analyzer, SourceRangeAt(file, content, origStart)))
	}

	// Homoglyph / mixed-script words.
	findings = append(findings, scanHomoglyphTokens(content, file, analyzer, seen)...)

	return findings
}

func firstSuspiciousInstructionOffset(decoded string, offsets []int, base int) (int, bool) {
	if loc := evalSystemRe.FindStringIndex(decoded); loc != nil {
		return base + offsets[loc[0]], true
	}
	if loc := suspiciousCallRe.FindStringIndex(decoded); loc != nil {
		return base + offsets[loc[0]], true
	}
	low := strings.ToLower(decoded)
	best := -1
	for _, tok := range suspiciousTokens {
		idx := strings.Index(low, tok)
		if idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	if best >= 0 {
		return base + offsets[best], true
	}
	return 0, false
}

func scanHomoglyphTokens(content, file, analyzer string, seen map[int]bool) []Finding {
	var findings []Finding
	for _, loc := range wordRe.FindAllStringIndex(content, -1) {
		if seen[loc[0]] {
			continue
		}
		word := content[loc[0]:loc[1]]
		mapped := homoglyphToLatin(word)

		scripts := detectScripts(word)
		if len(scripts) > 1 {
			seen[loc[0]] = true
			findings = append(findings, NewFinding(FindingObfuscatedInstruction,
				fmt.Sprintf("obfuscated instruction: mixed-script homoglyph (%s)", strings.Join(scripts, ", ")),
				analyzer, SourceRangeAt(file, content, loc[0])))
			continue
		}

		if mapped != word && (hasSuspiciousToken(mapped) || evalSystemRe.MatchString(mapped) || suspiciousCallRe.MatchString(mapped)) {
			seen[loc[0]] = true
			findings = append(findings, NewFinding(FindingObfuscatedInstruction,
				fmt.Sprintf("obfuscated instruction: homoglyph maps to %q", truncate(mapped, 40)),
				analyzer, SourceRangeAt(file, content, loc[0])))
		}
	}
	return findings
}

func homoglyphToLatin(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if l, ok := homoglyphMap[r]; ok {
			sb.WriteRune(l)
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

var homoglyphMap = map[rune]rune{
	// Cyrillic lookalikes.
	'а': 'a', 'А': 'A',
	'в': 'b', 'В': 'B',
	'е': 'e', 'Е': 'E',
	'к': 'k', 'К': 'K',
	'м': 'm', 'М': 'M',
	'о': 'o', 'О': 'O',
	'р': 'p', 'Р': 'P',
	'с': 'c', 'С': 'C',
	'т': 't', 'Т': 'T',
	'х': 'x', 'Х': 'X',
	'у': 'y', 'У': 'Y',
	// Greek lookalikes.
	'α': 'a', 'Α': 'A',
	'β': 'b', 'Β': 'B',
	'ε': 'e', 'Ε': 'E',
	'ο': 'o', 'Ο': 'O',
	'ρ': 'p', 'Ρ': 'P',
	'σ': 's', 'Σ': 'S',
	'χ': 'x', 'Χ': 'X',
	'γ': 'y', 'Γ': 'Y',
	'υ': 'y', 'Υ': 'Y',
	'ν': 'v', 'Ν': 'V',
	'ζ': 'z', 'Ζ': 'Z',
}

// detectScripts returns the script names present in a word.
func detectScripts(word string) []string {
	scripts := make(map[string]bool)
	for _, r := range word {
		script := runeScript(r)
		if script != "" {
			scripts[script] = true
		}
	}
	if len(scripts) < 2 {
		return nil
	}
	var names []string
	for name := range scripts {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

var scriptConfusables = []struct {
	name string
	rt   *unicode.RangeTable
}{
	{"Latin", unicode.Latin},
	{"Cyrillic", unicode.Cyrillic},
	{"Greek", unicode.Greek},
	{"Armenian", unicode.Armenian},
	{"Georgian", unicode.Georgian},
	{"Hebrew", unicode.Hebrew},
	{"Arabic", unicode.Arabic},
}

func runeScript(r rune) string {
	if unicode.Is(unicode.Common, r) || unicode.Is(unicode.Inherited, r) || !unicode.IsLetter(r) {
		return ""
	}
	for _, s := range scriptConfusables {
		if unicode.Is(s.rt, r) {
			return s.name
		}
	}
	if unicode.IsLetter(r) {
		return "Other"
	}
	return ""
}

func sortStrings(a []string) {
	for i := 0; i < len(a); i++ {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

// looksLikeInstruction checks whether decoded bytes contain a suspicious token
// and are fully printable ASCII.
func looksLikeInstruction(b []byte) ([]byte, bool) {
	if len(b) == 0 {
		return nil, false
	}
	for _, c := range b {
		if (c < 0x20 || c >= 0x7F) && c != '\n' && c != '\r' && c != '\t' {
			return nil, false
		}
	}
	s := string(b)
	if hasSuspiciousToken(s) {
		return b, true
	}
	return nil, false
}

// hasSuspiciousToken reports whether s contains any of the suspicious tokens.
func hasSuspiciousToken(s string) bool {
	low := strings.ToLower(s)
	for _, tok := range suspiciousTokens {
		if strings.Contains(low, tok) {
			return true
		}
	}
	return false
}

// token represents a URL/code-like token within content.
type token struct {
	start int
	text  string
}

// tokenizeURLLike extracts maximal runs of token characters from content.
func tokenizeURLLike(content string) []token {
	var toks []token
	i := 0
	for i < len(content) {
		if isTokenChar(content[i]) {
			start := i
			for i < len(content) && isTokenChar(content[i]) {
				i++
			}
			toks = append(toks, token{start: start, text: content[start:i]})
		} else {
			i++
		}
	}
	return toks
}

func isTokenChar(b byte) bool {
	if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') {
		return true
	}
	switch b {
	case '!', '$', '&', '\'', '(', ')', '*', '+', ',', '-', '.', '/', ':', ';',
		'=', '?', '@', '[', '\\', ']', '_', '~', '%', '#', '<', '>', '"':
		return true
	}
	return false
}

// decodeURLToken decodes percent, HTML entity and backslash-hex escapes inside
// a token and returns the decoded bytes, an offset map and whether any
// decoding happened. The offset map has length len(decoded)+1, where
// offsets[i] is the original byte offset of decoded byte i.
func decodeURLToken(tok string) (string, []int, bool) {
	var out strings.Builder
	offsets := make([]int, 0, len(tok)+1)
	changed := false
	i := 0
	for i < len(tok) {
		switch {
		case i+2 < len(tok) && tok[i] == '%' && isHex(tok[i+1]) && isHex(tok[i+2]):
			b, _ := hexByte(tok[i+1], tok[i+2])
			out.WriteByte(b)
			offsets = append(offsets, i)
			changed = true
			i += 3
		case i+3 < len(tok) && tok[i] == '\\' && tok[i+1] == 'x' && isHex(tok[i+2]) && isHex(tok[i+3]):
			b, _ := hexByte(tok[i+2], tok[i+3])
			out.WriteByte(b)
			offsets = append(offsets, i)
			changed = true
			i += 4
		case tok[i] == '&':
			end := findEntityEnd(tok, i)
			if end > i {
				entity := tok[i:end]
				repl := html.UnescapeString(entity)
				if repl != entity {
					out.WriteString(repl)
					for j := 0; j < len(repl); j++ {
						offsets = append(offsets, i)
					}
					changed = true
					i = end
					continue
				}
			}
			out.WriteByte(tok[i])
			offsets = append(offsets, i)
			i++
		default:
			out.WriteByte(tok[i])
			offsets = append(offsets, i)
			i++
		}
	}
	offsets = append(offsets, len(tok))
	return out.String(), offsets, changed
}

// decodeObfuscatedToken decodes the common encodings used in obfuscated code.
func decodeObfuscatedToken(tok string) (string, []int, bool) {
	// Fast path for whole-token base64 or 0x hex blocks.
	if decoded, ok := decodeBase64OrHexToken(tok); ok {
		offsets := make([]int, len(decoded)+1)
		for i := 0; i < len(offsets); i++ {
			offsets[i] = 0
		}
		offsets[len(offsets)-1] = len(tok)
		return string(decoded), offsets, true
	}

	// First pass: percent, HTML entities, backslash-hex and 0x hex blocks.
	s1, off1, changed := decodeSegmentEncodings(tok)

	// Second pass: decode base64-like blocks that may be embedded inside the
	// token (e.g. atob("ZXZhbA") or Z%58ZhbA). Skip it for plain base64 tokens
	// that were already handled by the fast path above.
	needsBase64 := changed
	if !needsBase64 {
		for j := 0; j < len(s1); j++ {
			if !isBase64Byte(s1[j]) {
				needsBase64 = true
				break
			}
		}
	}
	if !needsBase64 {
		return s1, off1, false
	}
	s2, off2, bchanged := decodeBase64Blocks(s1, off1)
	return s2, off2, changed || bchanged
}

// decodeSegmentEncodings decodes percent, HTML entity, backslash-hex and 0x
// hex escapes inside a token. It returns the decoded string, an offset map
// whose length is len(decoded)+1, and whether any decoding happened.
func decodeSegmentEncodings(tok string) (string, []int, bool) {
	var out bytes.Buffer
	offsets := make([]int, 0, len(tok)+1)
	changed := false
	i := 0
	for i < len(tok) {
		switch {
		case tryDecodeHexBlock(tok, &i, &out, &offsets):
			changed = true
		case i+2 < len(tok) && tok[i] == '%' && isHex(tok[i+1]) && isHex(tok[i+2]):
			b, _ := hexByte(tok[i+1], tok[i+2])
			out.WriteByte(b)
			offsets = append(offsets, i)
			changed = true
			i += 3
		case i+3 < len(tok) && tok[i] == '\\' && tok[i+1] == 'x' && isHex(tok[i+2]) && isHex(tok[i+3]):
			b, _ := hexByte(tok[i+2], tok[i+3])
			out.WriteByte(b)
			offsets = append(offsets, i)
			changed = true
			i += 4
		case tok[i] == '&':
			end := findEntityEnd(tok, i)
			if end > i {
				entity := tok[i:end]
				repl := html.UnescapeString(entity)
				if repl != entity {
					out.WriteString(repl)
					for j := 0; j < len(repl); j++ {
						offsets = append(offsets, i)
					}
					changed = true
					i = end
					continue
				}
			}
			out.WriteByte(tok[i])
			offsets = append(offsets, i)
			i++
		default:
			out.WriteByte(tok[i])
			offsets = append(offsets, i)
			i++
		}
	}
	offsets = append(offsets, len(tok))
	return out.String(), offsets, changed
}

// decodeBase64Blocks scans s for maximal base64-like runs and decodes any run
// that contains a suspicious instruction. The offset map has the same contract
// as decodeSegmentEncodings. Only the maximal run is tried, so the scan is
// linear in the token length.
func decodeBase64Blocks(s string, offsets []int) (string, []int, bool) {
	var out bytes.Buffer
	off := make([]int, 0, len(s)+1)
	changed := false
	i := 0
	for i < len(s) {
		if !isBase64Byte(s[i]) {
			out.WriteByte(s[i])
			off = append(off, offsets[i])
			i++
			continue
		}
		// Find the maximal base64 run starting at i.
		runEnd := i
		for runEnd < len(s) && isBase64Byte(s[runEnd]) {
			runEnd++
		}
		runLen := runEnd - i
		if runLen < 4 {
			for ; i < runEnd; i++ {
				out.WriteByte(s[i])
				off = append(off, offsets[i])
			}
			continue
		}
		if b, ok := tryBase64Decode(s[i:runEnd]); ok {
			if dec, ok := looksLikeInstruction(b); ok {
				out.Write(dec)
				for range dec {
					off = append(off, offsets[i])
				}
				changed = true
				i = runEnd
				continue
			}
		}
		// Not a suspicious base64 block: copy the run unchanged.
		for ; i < runEnd; i++ {
			out.WriteByte(s[i])
			off = append(off, offsets[i])
		}
	}
	off = append(off, offsets[len(offsets)-1])
	return out.String(), off, changed
}

func tryDecodeHexBlock(tok string, i *int, out *bytes.Buffer, offsets *[]int) bool {
	if (*i)+3 >= len(tok) {
		return false
	}
	if tok[*i] != '0' || (tok[(*i)+1] != 'x' && tok[(*i)+1] != 'X') {
		return false
	}
	j := (*i) + 2
	for j < len(tok) && isHex(tok[j]) {
		j++
	}
	hexStr := tok[(*i)+2 : j]
	if len(hexStr) < 4 || len(hexStr)%2 != 0 {
		return false
	}
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return false
	}
	if dec, ok := looksLikeInstruction(b); ok {
		out.Write(dec)
		for range dec {
			*offsets = append(*offsets, *i)
		}
		*i = j
		return true
	}
	return false
}

func decodeBase64OrHexToken(tok string) ([]byte, bool) {
	if hexLikeRe.MatchString(tok) {
		s := tok
		if strings.HasPrefix(s, "0X") || strings.HasPrefix(s, "0x") {
			s = s[2:]
		}
		if len(s)%2 == 0 {
			b, err := hex.DecodeString(s)
			if err == nil {
				if dec, ok := looksLikeInstruction(b); ok {
					return dec, true
				}
			}
		}
	}
	if isBase64String(tok) {
		if b, ok := tryBase64Decode(tok); ok {
			if dec, ok := looksLikeInstruction(b); ok {
				return dec, true
			}
		}
	}
	return nil, false
}

func isBase64Byte(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/'
}

func isBase64String(s string) bool {
	if len(s) < 4 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isBase64Byte(c) {
			continue
		}
		if c == '=' {
			// padding may only appear at the end.
			for ; i < len(s); i++ {
				if s[i] != '=' {
					return false
				}
			}
			return true
		}
		return false
	}
	return true
}

func tryBase64Decode(tok string) ([]byte, bool) {
	if len(tok)%4 == 0 {
		b, err := base64.StdEncoding.DecodeString(tok)
		if err == nil {
			return b, true
		}
	}
	b, err := base64.RawStdEncoding.DecodeString(tok)
	if err == nil {
		return b, true
	}
	return nil, false
}

func findEntityEnd(s string, i int) int {
	// Find the terminating ';' within a reasonable window. HTML entity names
	// are short; numeric/hex entities are also short.
	maxLen := 32
	if i+2 >= len(s) {
		return i
	}
	j := i + 1
	if s[j] == '#' {
		j++
		if j < len(s) && (s[j] == 'x' || s[j] == 'X') {
			j++
		}
		for j < len(s) && j-i < maxLen {
			if s[j] == ';' {
				return j + 1
			}
			if !isHex(s[j]) && !isDecimalByte(s[j]) {
				break
			}
			j++
		}
		return i
	}
	// Named entity: letters/digits.
	for j < len(s) && j-i < maxLen {
		if s[j] == ';' {
			return j + 1
		}
		if !isEntityNameByte(s[j]) {
			break
		}
		j++
	}
	return i
}

func isDecimalByte(b byte) bool    { return b >= '0' && b <= '9' }
func isEntityNameByte(b byte) bool { return isASCIIAlnum(b) }

// isASCIIAlnum reports whether b is an ASCII letter or digit, i.e. a byte that
// cannot delimit a URL scheme or an HTML entity name.
func isASCIIAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func hexByte(a, b byte) (byte, bool) {
	if !isHex(a) || !isHex(b) {
		return 0, false
	}
	return hexCharValue(a)<<4 | hexCharValue(b), true
}

func hexCharValue(b byte) byte {
	switch {
	case b >= '0' && b <= '9':
		return b - '0'
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10
	}
	return 0
}
