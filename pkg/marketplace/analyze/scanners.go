package analyze

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	htmlCommentRe = regexp.MustCompile(`(?s:<!--.*?-->)`)

	dangerousURLRe = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])((?:javascript|data|file|vbscript):)`)

	wordRe = regexp.MustCompile(`\pL+`)

	base64LikeRe   = regexp.MustCompile(`[A-Za-z0-9+/]{8,}={0,2}`)
	hexLikeRe      = regexp.MustCompile(`(?:0x)?[0-9a-fA-F]{8,}`)
	pctEncodedRe   = regexp.MustCompile(`(?:%[0-9a-fA-F]{2}){3,}`)
	htmlHexEntRe   = regexp.MustCompile(`(?:&#x[0-9a-fA-F]+;){2,}`)
	backslashHexRe = regexp.MustCompile(`(?:\\x[0-9a-fA-F]{2}){2,}`)

	evalSystemRe = regexp.MustCompile(`(?i)\b(?:eval|system)\s*\(`)
)

// suspiciousTokenRe matches decoded strings that are likely hostile.
var suspiciousTokens = []string{
	"eval", "system", "exec", "script", "javascript", "document", "window",
	"fetch", "import", "require", "shell", "bash", "sh -c", "curl", "wget",
	"rm -rf", "<script", "onerror", "onload", "alert", "prompt", "console",
	"function", "fromcharcode", "atob", "unescape", "decodeuri", "filesystem",
	"cmd", "powershell", "python", "perl", "ruby",
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

// ScanTerminalControl finds C0 control bytes (except tab/newline/cr) and
// ANSI/CSI/OSC escape sequences.
func ScanTerminalControl(content, file, analyzer string) []Finding {
	var findings []Finding
	start := -1
	var kind byte
	for i := 0; i < len(content); {
		b := content[i]
		if b == 0x1B {
			// Try to consume a terminal escape sequence.
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
		isControl := b < 0x20 && b != '\t' && b != '\n' && b != '\r'
		if isControl {
			if start == -1 {
				start = i
				kind = 'C'
			}
			i++
			continue
		}
		if start != -1 {
			msg := terminalControlMessage(content, start, i, kind)
			findings = append(findings, NewFinding(FindingTerminalControl, msg, analyzer, SourceRangeAt(file, content, start)))
			start = -1
			kind = 0
		}
		i++
	}
	if start != -1 {
		msg := terminalControlMessage(content, start, len(content), kind)
		findings = append(findings, NewFinding(FindingTerminalControl, msg, analyzer, SourceRangeAt(file, content, start)))
	}
	return findings
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
		// OSC: terminated by BEL (0x07) or ST (ESC \\).
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
	prefix := "C0 control"
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

// ScanDangerousURLs finds URL schemes that can execute code or access local files.
func ScanDangerousURLs(content, file, analyzer string) []Finding {
	var findings []Finding
	for _, loc := range dangerousURLRe.FindAllStringSubmatchIndex(content, -1) {
		if len(loc) < 4 || loc[2] < 0 {
			continue
		}
		start, end := loc[2], loc[3]
		scheme := strings.ToLower(content[start : end-1])
		msg := fmt.Sprintf("dangerous URL scheme: %s:", scheme)
		findings = append(findings, NewFinding(FindingDangerousURL, msg, analyzer, SourceRangeAt(file, content, start)))
	}
	return findings
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

// ScanSuspiciousPaths finds path traversal, absolute paths and sensitive paths.
func ScanSuspiciousPaths(content, file, analyzer string) []Finding {
	var findings []Finding
	seen := make(map[int]bool)

	// Exact sensitive paths (checked first so they take precedence over the
	// absolute-path rule).
	sensitiveRe := regexp.MustCompile(`(?i)(/etc/passwd|/etc/shadow|~/.ssh)`)
	for _, loc := range sensitiveRe.FindAllStringIndex(content, -1) {
		if addSuspiciousPathFinding(&findings, seen, content, file, analyzer, loc[0], loc[1]) {
			seen[loc[0]] = true
		}
	}

	// Absolute Unix paths, but not double-slash (protocol-relative) or
	// slash-after-colon (scheme separators).
	absRe := regexp.MustCompile(`([\s"'(<\[\{=]|^)(/(?:[^/\s]+)(?:/[^/\s]+)*)`)
	for _, loc := range absRe.FindAllStringSubmatchIndex(content, -1) {
		if len(loc) < 6 {
			continue
		}
		start, end := loc[4], loc[5]
		if addSuspiciousPathFinding(&findings, seen, content, file, analyzer, start, end) {
			seen[start] = true
		}
	}

	// Runs of two or more dots (path traversal).
	dotRe := regexp.MustCompile(`\.{2,}`)
	for _, loc := range dotRe.FindAllStringIndex(content, -1) {
		if addSuspiciousPathFinding(&findings, seen, content, file, analyzer, loc[0], loc[1]) {
			seen[loc[0]] = true
		}
	}

	// Tilde home prefix.
	tildeRe := regexp.MustCompile(`~[\\/]`)
	for _, loc := range tildeRe.FindAllStringIndex(content, -1) {
		if addSuspiciousPathFinding(&findings, seen, content, file, analyzer, loc[0], loc[1]) {
			seen[loc[0]] = true
		}
	}

	// Windows drive letters and UNC prefixes. Require a boundary before the
	// drive letter so that URL schemes like "https://" are not flagged.
	driveRe := regexp.MustCompile(`(?i)(?:^|[^A-Za-z])([a-z]:[\\/])`)
	for _, loc := range driveRe.FindAllStringSubmatchIndex(content, -1) {
		if len(loc) < 4 {
			continue
		}
		start, end := loc[2], loc[3]
		if addSuspiciousPathFinding(&findings, seen, content, file, analyzer, start, end) {
			seen[start] = true
		}
	}

	uncRe := regexp.MustCompile(`\\\\`)
	for _, loc := range uncRe.FindAllStringIndex(content, -1) {
		if addSuspiciousPathFinding(&findings, seen, content, file, analyzer, loc[0], loc[1]) {
			seen[loc[0]] = true
		}
	}

	return findings
}

func addSuspiciousPathFinding(findings *[]Finding, seen map[int]bool, content, file, analyzer string, start, end int) bool {
	if start < 0 || end > len(content) || seen[start] {
		return false
	}
	match := content[start:end]
	msg := fmt.Sprintf("suspicious path: %q", truncate(match, 80))
	*findings = append(*findings, NewFinding(FindingSuspiciousPath, msg, analyzer, SourceRangeAt(file, content, start)))
	return true
}

// ScanObfuscatedInstructions finds encoded or obfuscated hostile instructions.
func ScanObfuscatedInstructions(content, file, analyzer string) []Finding {
	var findings []Finding
	seen := make(map[int]bool)

	for _, loc := range evalSystemRe.FindAllStringIndex(content, -1) {
		findings = append(findings, NewFinding(FindingObfuscatedInstruction,
			fmt.Sprintf("obfuscated instruction: %q", truncate(content[loc[0]:loc[1]], 40)),
			analyzer, SourceRangeAt(file, content, loc[0])))
		seen[loc[0]] = true
	}

	findings = append(findings, scanBase64Blocks(content, file, analyzer, seen)...)
	findings = append(findings, scanHexBlocks(content, file, analyzer, seen)...)
	findings = append(findings, scanPercentEncoded(content, file, analyzer, seen)...)
	findings = append(findings, scanHTMLHexEntities(content, file, analyzer, seen)...)
	findings = append(findings, scanBackslashHex(content, file, analyzer, seen)...)
	findings = append(findings, scanMixedScriptHomoglyphs(content, file, analyzer, seen)...)

	return findings
}

func scanBase64Blocks(content, file, analyzer string, seen map[int]bool) []Finding {
	var findings []Finding
	for _, loc := range base64LikeRe.FindAllStringIndex(content, -1) {
		if seen[loc[0]] {
			continue
		}
		tok := content[loc[0]:loc[1]]
		if len(tok)%4 != 0 {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(tok)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(tok)
			if err != nil {
				continue
			}
		}
		if decoded, ok := looksLikeInstruction(decoded); ok {
			seen[loc[0]] = true
			findings = append(findings, NewFinding(FindingObfuscatedInstruction,
				fmt.Sprintf("obfuscated instruction: base64 decodes to %q", truncate(string(decoded), 60)),
				analyzer, SourceRangeAt(file, content, loc[0])))
		}
	}
	return findings
}

func scanHexBlocks(content, file, analyzer string, seen map[int]bool) []Finding {
	var findings []Finding
	for _, loc := range hexLikeRe.FindAllStringIndex(content, -1) {
		if seen[loc[0]] {
			continue
		}
		tok := content[loc[0]:loc[1]]
		if strings.HasPrefix(strings.ToLower(tok), "0x") {
			tok = tok[2:]
		}
		if len(tok)%2 != 0 {
			continue
		}
		decoded, err := hex.DecodeString(tok)
		if err != nil {
			continue
		}
		if decoded, ok := looksLikeInstruction(decoded); ok {
			seen[loc[0]] = true
			findings = append(findings, NewFinding(FindingObfuscatedInstruction,
				fmt.Sprintf("obfuscated instruction: hex decodes to %q", truncate(string(decoded), 60)),
				analyzer, SourceRangeAt(file, content, loc[0])))
		}
	}
	return findings
}

func scanPercentEncoded(content, file, analyzer string, seen map[int]bool) []Finding {
	var findings []Finding
	for _, loc := range pctEncodedRe.FindAllStringIndex(content, -1) {
		if seen[loc[0]] {
			continue
		}
		tok := content[loc[0]:loc[1]]
		decoded, err := url.PathUnescape(tok)
		if err != nil {
			continue
		}
		if hasSuspiciousToken(decoded) {
			seen[loc[0]] = true
			findings = append(findings, NewFinding(FindingObfuscatedInstruction,
				fmt.Sprintf("obfuscated instruction: percent-decodes to %q", truncate(decoded, 60)),
				analyzer, SourceRangeAt(file, content, loc[0])))
		}
	}
	return findings
}

func scanHTMLHexEntities(content, file, analyzer string, seen map[int]bool) []Finding {
	var findings []Finding
	entityRe := regexp.MustCompile(`&#x[0-9a-fA-F]+;`)
	for _, loc := range htmlHexEntRe.FindAllStringIndex(content, -1) {
		if seen[loc[0]] {
			continue
		}
		tok := content[loc[0]:loc[1]]
		var sb strings.Builder
		for _, m := range entityRe.FindAllStringSubmatchIndex(tok, -1) {
			hex := tok[m[0]+3 : m[1]-1]
			n, err := strconv.ParseInt(hex, 16, 32)
			if err != nil {
				continue
			}
			sb.WriteRune(rune(n))
		}
		if sb.Len() > 0 && hasSuspiciousToken(sb.String()) {
			seen[loc[0]] = true
			findings = append(findings, NewFinding(FindingObfuscatedInstruction,
				fmt.Sprintf("obfuscated instruction: HTML hex entities decode to %q", truncate(sb.String(), 60)),
				analyzer, SourceRangeAt(file, content, loc[0])))
		}
	}
	return findings
}

func scanBackslashHex(content, file, analyzer string, seen map[int]bool) []Finding {
	var findings []Finding
	hexRe := regexp.MustCompile(`\\x([0-9a-fA-F]{2})`)
	for _, loc := range backslashHexRe.FindAllStringIndex(content, -1) {
		if seen[loc[0]] {
			continue
		}
		tok := content[loc[0]:loc[1]]
		var sb strings.Builder
		for _, m := range hexRe.FindAllStringSubmatchIndex(tok, -1) {
			hex := tok[m[2]:m[3]]
			n, err := strconv.ParseInt(hex, 16, 8)
			if err != nil {
				continue
			}
			sb.WriteByte(byte(n))
		}
		if sb.Len() > 0 && hasSuspiciousToken(sb.String()) {
			seen[loc[0]] = true
			findings = append(findings, NewFinding(FindingObfuscatedInstruction,
				fmt.Sprintf("obfuscated instruction: \\xNN escapes decode to %q", truncate(sb.String(), 60)),
				analyzer, SourceRangeAt(file, content, loc[0])))
		}
	}
	return findings
}

func scanMixedScriptHomoglyphs(content, file, analyzer string, seen map[int]bool) []Finding {
	var findings []Finding
	for _, loc := range wordRe.FindAllStringIndex(content, -1) {
		if seen[loc[0]] {
			continue
		}
		word := content[loc[0]:loc[1]]
		if scripts := detectScripts(word); len(scripts) > 1 {
			seen[loc[0]] = true
			findings = append(findings, NewFinding(FindingObfuscatedInstruction,
				fmt.Sprintf("obfuscated instruction: mixed-script homoglyph (%s)", strings.Join(scripts, ", ")),
				analyzer, SourceRangeAt(file, content, loc[0])))
		}
	}
	return findings
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
// and are mostly printable ASCII.
func looksLikeInstruction(b []byte) ([]byte, bool) {
	if len(b) == 0 {
		return nil, false
	}
	printable := 0
	for _, c := range b {
		if c >= 0x20 && c < 0x7F || c == '\n' || c == '\r' || c == '\t' {
			printable++
		}
	}
	if printable*100/len(b) < 70 {
		return nil, false
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
