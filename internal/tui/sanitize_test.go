package tui

import (
	"errors"
	"testing"
	"unicode/utf8"

	"github.com/Adyen/agent-marketplace/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func TestSanitizeTerminalDataFixtures(t *testing.T) {
	for _, test := range []struct {
		name  string
		mode  session.TerminalDataMode
		input string
		want  string
	}{
		{name: "ANSI SGR", mode: terminalSingleLine, input: "safe\x1b[31mred\x1b[0m", want: `safe\x1B[31mred\x1B[0m`},
		{name: "OSC title and BEL", mode: terminalSingleLine, input: "safe\x1b]0;owned\x07tail", want: `safe\x1B]0;owned\x07tail`},
		{name: "C0 structure", mode: terminalSingleLine, input: "a\x00\tb\rc\nd\x7f", want: `a\x00\x09b\x0Dc\x0Ad\x7F`},
		{name: "C1 controls", mode: terminalSingleLine, input: "a\u0085b\u009bc", want: `a\x85b\x9Bc`},
		{name: "bidi controls", mode: terminalSingleLine, input: "a\u061cb\u200fc\u202ed\u2066e", want: `a\u061Cb\u200Fc\u202Ed\u2066e`},
		{name: "Unicode separators", mode: terminalSingleLine, input: "a\u2028b\u2029c", want: `a\u2028b\u2029c`},
		{name: "markdown structure", mode: terminalMarkdownBody, input: "# Heading\nline\tvalue\r\n", want: "# Heading\nline\\x09value\\x0D\n"},
		{name: "printable Unicode", mode: terminalSingleLine, input: "A界e\u0301 👩‍💻", want: "A界e\u0301 👩‍💻"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := sanitizeTerminalData(test.input, test.mode); got != test.want {
				t.Fatalf("sanitizeTerminalData(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestNeedsSanitizingAgreesWithTransform(t *testing.T) {
	for _, test := range []struct {
		value string
		mode  session.TerminalDataMode
	}{
		{value: "ordinary printable text", mode: terminalSingleLine},
		{value: "line one\nline two", mode: terminalMarkdownBody},
		{value: "line one\nline two", mode: terminalSingleLine},
		{value: "unsafe\x1b[2J", mode: terminalSingleLine},
		{value: "bidi\u202e", mode: terminalSingleLine},
		{value: string([]byte{'x', 0xff, 'y'}), mode: terminalSingleLine},
		{value: "encoded &#27;[2J", mode: terminalMarkdownBody},
		{value: "benign &amp; entity", mode: terminalMarkdownBody},
	} {
		changed := sanitizeTerminalData(test.value, test.mode) != test.value
		if got := session.TerminalDataNeedsSanitizing(test.value, test.mode); got != changed {
			t.Fatalf("needsSanitizing(%q, %v) = %v, transform changed=%v", test.value, test.mode, got, changed)
		}
	}
}

func TestSanitizedPrintableUnicodeRetainsPhysicalWidth(t *testing.T) {
	input := "A界e\u0301"
	got := sanitizeTerminalData(input, terminalSingleLine)
	if got != input || !utf8.ValidString(got) {
		t.Fatalf("printable Unicode changed: %q", got)
	}
	if width := ansi.StringWidth(got); width != 4 {
		t.Fatalf("physical width = %d, want 4 for %q", width, got)
	}
}

func TestMarkdownEntityBoundaryTransformsUnsafeReferencesOnly(t *testing.T) {
	input := "plain &#27 no-semicolon\n" +
		`[link](https://example.invalid/&#27;[2J "title &#x202E;")` + "\n" +
		`<span title="&NewLine;">&Tab;</span>` + "\n" +
		"overflow &#4294967323; benign &amp; &#65; &copy;"
	got := sanitizeMarkdownForTerminal(input)
	for _, want := range []string{`plain \x1B no-semicolon`, `https://example.invalid/\x1B[2J`, `title \u202E`, `title="\x0A"`, `>\x09</span>`, `overflow \uFFFD`, `benign &amp; &#65; &copy;`} {
		if !contains(got, want) {
			t.Fatalf("entity-aware Markdown boundary missing %q:\n%s", want, got)
		}
	}
}

func TestSanitizeReturnedErrorPreservesIdentity(t *testing.T) {
	original := errors.New("unsafe\n\x1b[2J")
	sanitized := sanitizeReturnedError(original)
	if !errors.Is(sanitized, original) {
		t.Fatal("sanitized error lost identity")
	}
	if got := sanitized.Error(); got != `unsafe\x0A\x1B[2J` {
		t.Fatalf("sanitized error = %q", got)
	}
}
