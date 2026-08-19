package analyze

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	htmlCommentRe = regexp.MustCompile(`(?s:<!--.*?-->)`)

	dangerousURLRe = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])((?:javascript|data|file|vbscript):)`)

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

// ScanDangerousURLs finds URL schemes that can execute code or access local files.
func ScanDangerousURLs(content, file, analyzer string) []Finding {
	var findings []Finding
	seen := make(map[int]bool)

	for _, tok := range tokenizeURLLike(content) {
		if !looksLikeDangerousURLToken(tok.text) {
			continue
		}
		decoded, offsets, _ := decodeURLToken(tok.text)

		loc := dangerousURLRe.FindStringSubmatchIndex(decoded)
		if len(loc) < 4 || loc[2] < 0 {
			continue
		}

		origStart := tok.start + offsets[loc[2]]
		if seen[origStart] {
			continue
		}
		seen[origStart] = true

		schemeEnd := loc[3] - 1
		if schemeEnd < loc[2] {
			schemeEnd = loc[3]
		}
		scheme := strings.ToLower(decoded[loc[2]:schemeEnd])
		msg := fmt.Sprintf("dangerous URL scheme: %s:", scheme)
		findings = append(findings, NewFinding(FindingDangerousURL, msg, analyzer, SourceRangeAt(file, content, origStart)))
	}

	return findings
}

func looksLikeDangerousURLToken(tok string) bool {
	if strings.Contains(tok, ":") {
		return true
	}
	for i := 0; i < len(tok); i++ {
		switch tok[i] {
		case '%':
			if i+2 < len(tok) && isHex(tok[i+1]) && isHex(tok[i+2]) {
				return true
			}
		case '&':
			return true
		case '\\':
			if i+3 < len(tok) && tok[i+1] == 'x' && isHex(tok[i+2]) && isHex(tok[i+3]) {
				return true
			}
		}
	}
	return false
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

	scanSuspiciousPathString(&findings, seen, content, file, analyzer, content, 0, nil)

	if containsEncodedPathMarker(content) {
		for _, tok := range tokenizeURLLike(content) {
			if !looksLikeEncodedPath(tok.text) {
				continue
			}
			decoded, offsets, changed := decodeURLToken(tok.text)
			if !changed {
				continue
			}
			scanSuspiciousPathString(&findings, seen, content, file, analyzer, decoded, tok.start, offsets)
		}
	}

	return findings
}

func containsEncodedPathMarker(s string) bool {
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

func scanSuspiciousPathString(findings *[]Finding, seen map[int]bool, content, file, analyzer, s string, base int, offsets []int) {
	run := func(re *regexp.Regexp, group int) {
		if offsets == nil {
			if group > 0 {
				for _, loc := range re.FindAllStringSubmatchIndex(s, -1) {
					if len(loc) <= group*2+1 || loc[group*2] < 0 {
						continue
					}
					addSuspiciousPathFinding(findings, seen, content, file, analyzer, loc[group*2], loc[group*2+1])
				}
				return
			}
			for _, loc := range re.FindAllStringIndex(s, -1) {
				addSuspiciousPathFinding(findings, seen, content, file, analyzer, loc[0], loc[1])
			}
			return
		}
		if group > 0 {
			for _, loc := range re.FindAllStringSubmatchIndex(s, -1) {
				if len(loc) <= group*2+1 || loc[group*2] < 0 {
					continue
				}
				addDecodedSuspiciousPath(findings, seen, content, file, analyzer, s, base, offsets, loc[group*2], loc[group*2+1])
			}
			return
		}
		for _, loc := range re.FindAllStringIndex(s, -1) {
			addDecodedSuspiciousPath(findings, seen, content, file, analyzer, s, base, offsets, loc[0], loc[1])
		}
	}

	sensitiveRe := regexp.MustCompile(`(?i)(/etc/passwd|/etc/shadow|~/.ssh)`)
	run(sensitiveRe, 0)

	absRe := regexp.MustCompile(`([\s"'(<\[\{=]|^)(/(?:[^/\s]+)(?:/[^/\s]+)*)`)
	run(absRe, 2)

	dotRe := regexp.MustCompile(`\.{2,}`)
	run(dotRe, 0)

	tildeRe := regexp.MustCompile(`~[\\/]`)
	run(tildeRe, 0)

	driveRe := regexp.MustCompile(`(?i)(?:^|[^A-Za-z])([a-z]:[\\/])`)
	run(driveRe, 1)

	uncRe := regexp.MustCompile(`\\\\`)
	run(uncRe, 0)
}

func addDecodedSuspiciousPath(findings *[]Finding, seen map[int]bool, content, file, analyzer, s string, base int, offsets []int, decStart, decEnd int) bool {
	if decStart < 0 || decEnd > len(s) || offsets == nil {
		return false
	}
	start := base + offsets[decStart]
	end := base + offsets[len(offsets)-1]
	if decEnd < len(offsets) {
		end = base + offsets[decEnd]
	}
	return addSuspiciousPathFinding(findings, seen, content, file, analyzer, start, end)
}

func looksLikeEncodedPath(tok string) bool {
	for i := 0; i < len(tok); i++ {
		switch tok[i] {
		case '%':
			if i+2 < len(tok) && isHex(tok[i+1]) && isHex(tok[i+2]) {
				return true
			}
		case '&':
			return true
		case '\\':
			if i+3 < len(tok) && tok[i+1] == 'x' && isHex(tok[i+2]) && isHex(tok[i+3]) {
				return true
			}
		}
	}
	return false
}

func addSuspiciousPathFinding(findings *[]Finding, seen map[int]bool, content, file, analyzer string, start, end int) bool {
	if start < 0 || end > len(content) || seen[start] {
		return false
	}
	match := content[start:end]
	msg := fmt.Sprintf("suspicious path: %q", truncate(match, 80))
	*findings = append(*findings, NewFinding(FindingSuspiciousPath, msg, analyzer, SourceRangeAt(file, content, start)))
	seen[start] = true
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

func isDecimalByte(b byte) bool { return b >= '0' && b <= '9' }
func isEntityNameByte(b byte) bool {
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
