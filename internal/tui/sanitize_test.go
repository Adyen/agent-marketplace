package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
)

func TestSanitizeTerminalDataFixtures(t *testing.T) {
	cases := []struct {
		name  string
		mode  terminalDataMode
		input string
		want  string
	}{
		{name: "ANSI SGR", mode: terminalSingleLine, input: "safe\x1b[31mred\x1b[0m", want: `safe\x1B[31mred\x1B[0m`},
		{name: "OSC title and BEL", mode: terminalSingleLine, input: "safe\x1b]0;owned\x07tail", want: `safe\x1B]0;owned\x07tail`},
		{name: "C0 structure", mode: terminalSingleLine, input: "a\x00\tb\rc\nd\x7f", want: `a\x00\x09b\x0Dc\x0Ad\x7F`},
		{name: "C1 controls", mode: terminalSingleLine, input: "a\u0085b\u009bc", want: `a\x85b\x9Bc`},
		{name: "bidi controls", mode: terminalSingleLine, input: "a\u061cb\u200fc\u202ed\u2066e", want: `a\u061Cb\u200Fc\u202Ed\u2066e`},
		{name: "Unicode line separators", mode: terminalSingleLine, input: "a\u2028b\u2029c", want: `a\u2028b\u2029c`},
		{name: "markdown structure", mode: terminalMarkdownBody, input: "# Heading\nline\tvalue\r\n", want: "# Heading\nline\\x09value\\x0D\n"},
		{name: "printable Unicode", mode: terminalSingleLine, input: "A界e\u0301 👩‍💻", want: "A界e\u0301 👩‍💻"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeTerminalData(tc.input, tc.mode); got != tc.want {
				t.Fatalf("sanitizeTerminalData(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
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

func TestUntrustedFieldsCannotCreateTerminalStructureOrChrome(t *testing.T) {
	r := mkRow("safe\nConfirm\ny = yes\x1b[2J\u202e", withProject, withRegistry("registry\x1b]0;owned\x07.git", "registry\rname"))
	r.Artifact.RegistryURL = "registry\x1b]0;owned\x07.git"
	r.Artifact.Path = "/cache/path\nq quit\x1b[10;10H"
	r.Artifact.Description = "Sync complete\n[enter] confirm\x1b[31mstyled\x1b[?1049l"
	m := modelWithSelectedRow(t, r)
	m.driftAssessment = driftReady
	m.width = 100
	m.height = 24
	output := m.View()
	for _, unsafe := range []string{"\x1b]0;owned", "\x1b[2J", "\x1b[10;10H", "\x1b[31m", "\x1b[?1049l", "\u202e", "\r"} {
		if strings.Contains(output, unsafe) {
			t.Fatalf("rendered unsafe terminal structure %q:\n%q", unsafe, output)
		}
	}
	for _, escaped := range []string{`\x1B]0;owned\x07`, `\x0AConfirm\x0Ay = yes\x1B[2J\u202E`, `\x0Aq quit\x1B[10;10H`, `\x1B[31mstyled\x1B[?1049l`} {
		if !strings.Contains(output, escaped) {
			t.Fatalf("render omitted readable escape %q:\n%s", escaped, ansi.Strip(output))
		}
	}
	if strings.Contains(output, "\nConfirm\n") || !strings.Contains(ansi.Strip(output), "agent-marketplace") {
		t.Fatalf("untrusted data spoofed chrome or damaged trusted header anchor:\n%s", ansi.Strip(output))
	}
}

func TestDiagnosticAndConfigPathBoundariesNeutralizeControls(t *testing.T) {
	m := terminalContractModel(t)
	m.width = 100
	m.height = 30
	m.diagnostics.replaceSnapshot([]tuiDiagnostic{snapshotDiagnostic(
		diagnosticError,
		diagnosticConfig,
		errors.New("failure\nErrors (99)\x1b]2;owned\x07\u202e"),
	)})
	m.pane = paneErrors
	output := m.View()
	for _, unsafe := range []string{"\x1b]2;owned", "\u202e", "\nErrors (99)"} {
		if strings.Contains(output, unsafe) {
			t.Fatalf("diagnostic boundary emitted %q: %q", unsafe, output)
		}
	}
	for _, want := range []string{`failure\x0AErrors (99)\x1B]2;owned\x07\u202E`, "Errors (1)", "esc close"} {
		if !strings.Contains(ansi.Strip(output), want) {
			t.Fatalf("diagnostic boundary missing %q:\n%s", want, ansi.Strip(output))
		}
	}

	m.pane = paneMigrationModal
	m.migrationPrompt = migrationPrompt{configs: []migrationConfig{{label: "Project", path: "/repo\n[enter] fake\x1b[2J"}}}
	output = renderMigrationModal(m)
	if strings.Contains(output, "\x1b[2J") || strings.Contains(output, "\n[enter] fake") {
		t.Fatalf("config path changed modal structure: %q", output)
	}
	if !strings.Contains(output, `/repo\x0A[enter] fake\x1B[2J`) {
		t.Fatalf("config path did not render readable escapes:\n%s", ansi.Strip(output))
	}
}

func TestMarkdownBoundaryPreservesStructureAndNeutralizesControls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resource.md")
	content := "# 标题 e\u0301\n\nReadable **Markdown**.\n\x1b]0;owned\x07\n\x1b[2Jfalse prompt\n\u202ereordered"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	r := mkRow("markdown", withProject)
	r.Artifact.Path = path
	out := renderDetailFullContent(r, 80)
	stripped := ansi.Strip(out)
	for _, want := range []string{"标题", "e\u0301", "Readable", `\x1B]0;owned\x07`, `\x1B[2Jfalse prompt`, `\u202Ereordered`} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("sanitized Markdown missing %q:\n%s", want, stripped)
		}
	}
	for _, unsafe := range []string{"\x1b]0;owned", "\x1b[2J", "\u202e"} {
		if strings.Contains(out, unsafe) {
			t.Fatalf("Markdown emitted unsafe terminal control %q: %q", unsafe, out)
		}
	}
}

func TestMarkdownEntityBoundaryTransformsUnsafeReferencesOnly(t *testing.T) {
	input := "plain &#27 no-semicolon\n" +
		`[link](https://example.invalid/&#27;[2J "title &#x202E;")` + "\n" +
		`<span title="&NewLine;">&Tab;</span>` + "\n" +
		"overflow &#4294967323; benign &amp; &#65; &copy;"
	got := sanitizeMarkdownForTerminal(input)
	for _, want := range []string{
		`plain \x1B no-semicolon`,
		`https://example.invalid/\x1B[2J`,
		`title \u202E`,
		`title="\x0A"`,
		`>\x09</span>`,
		`overflow \uFFFD`,
		`benign &amp; &#65; &copy;`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("entity-aware Markdown boundary missing %q:\n%s", want, got)
		}
	}
}

func TestMarkdownEntityControlsAreNeutralizedInEveryRenderedContext(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		visible []string
	}{
		{
			name: "plain text",
			input: "plain osc52=&#27;]52;c;c2VjcmV0&#7; alt=&#27;[?1049h " +
				"c0=&#9;/&#13;/&#127; c1=&#129;/&#155;/&#157; " +
				"bidi=&#x202E; newline=&#10; done",
			visible: []string{`\x1B]52;c;c2VjcmV0\x07`, `\x1B[?1049h`, `\x09`, `\x0D`, `\x7F`, `\x81`, `\x9B`, `\x9D`, `\u202E`, `\x0A`},
		},
		{
			name:    "emphasis",
			input:   `*emphasis &#x1B;]52;c;ZW1waGFzaXM=&#x07; and &#27;[?1049l*`,
			visible: []string{`\x1B]52;c;ZW1waGFzaXM=\x07`, `\x1B[?1049l`},
		},
		{
			name:    "code span",
			input:   "`code &#27;[2J and &#157;52;c;Y29kZQ==&#7;`",
			visible: []string{`\x1B[2J`, `\x9D52;c;Y29kZQ==\x07`},
		},
		{
			name:    "link text destination and title",
			input:   `[link &#27;]52;c;bGluaw==&#7;](https://example.invalid/&#27;[2J "title &#27;[?1049h &#x202E;")`,
			visible: []string{`\x1B]52;c;bGluaw==\x07`},
		},
		{
			name:    "raw HTML",
			input:   `<span title="&#27;[?1049h">raw &#27;]52;c;aHRtbA==&#7; &#x202E;</span>`,
			visible: []string{`\x1B]52;c;aHRtbA==\x07`, `\u202E`},
		},
		{
			name:    "named entities",
			input:   `named &Tab; &NewLine; &lrm; &rlm;`,
			visible: []string{`\x09`, `\x0A`, `\u200E`, `\u200F`},
		},
		{
			name:    "invalid UTF-8",
			input:   string([]byte("invalid \xff &#27;]52;c;aW52YWxpZA==&#7;")),
			visible: []string{`\xFF`, `\x1B]52;c;aW52YWxpZA==\x07`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := renderMarkdownWithStyle(tc.input, glamour.WithStandardStyle("dark"))
			if err != nil {
				t.Fatalf("render Markdown: %v", err)
			}
			assertOnlyTrustedMarkdownControls(t, out)
			if !containsSGR(out) {
				t.Fatalf("trusted renderer SGR was removed: %q", out)
			}
			stripped := ansi.Strip(out)
			for _, want := range tc.visible {
				if !strings.Contains(stripped, want) {
					t.Fatalf("final render missing visible escape %q:\n%s", want, stripped)
				}
			}
		})
	}
}

func assertOnlyTrustedMarkdownControls(t *testing.T, rendered string) {
	t.Helper()
	for offset := 0; offset < len(rendered); {
		b := rendered[offset]
		if b == '\x1b' {
			if offset+2 >= len(rendered) || rendered[offset+1] != '[' {
				t.Fatalf("final render contains non-CSI escape at byte %d: %q", offset, rendered)
			}
			end := offset + 2
			for end < len(rendered) && (rendered[end] < 0x40 || rendered[end] > 0x7e) {
				end++
			}
			if end >= len(rendered) || rendered[end] != 'm' {
				t.Fatalf("final render contains non-SGR CSI at byte %d: %q", offset, rendered)
			}
			offset = end + 1
			continue
		}
		if b < utf8.RuneSelf {
			if (b < 0x20 && b != '\n') || b == 0x7f {
				t.Fatalf("final render contains untrusted control 0x%02X at byte %d: %q", b, offset, rendered)
			}
			offset++
			continue
		}
		r, size := utf8.DecodeRuneInString(rendered[offset:])
		if r == utf8.RuneError && size == 1 {
			t.Fatalf("final render contains invalid UTF-8 at byte %d: %q", offset, rendered)
		}
		if (r >= 0x80 && r <= 0x9f) || isTerminalBidiControl(r) || r == 0x2028 || r == 0x2029 {
			t.Fatalf("final render contains untrusted control U+%04X at byte %d: %q", r, offset, rendered)
		}
		offset += size
	}
}

func containsSGR(rendered string) bool {
	for offset := 0; offset+2 < len(rendered); offset++ {
		if rendered[offset] != '\x1b' || rendered[offset+1] != '[' {
			continue
		}
		for end := offset + 2; end < len(rendered); end++ {
			if rendered[end] >= 0x40 && rendered[end] <= 0x7e {
				return rendered[end] == 'm'
			}
		}
	}
	return false
}
