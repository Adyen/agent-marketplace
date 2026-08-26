package analyze

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestScanHTMLComments(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"single", "before <!-- hidden --> after", 1},
		{"multiline", "<!--\nsecret\n-->", 1},
		{"none", "no comments here", 0},
		{"two", "<!-- one --> <!-- two -->", 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanHTMLComments(tc.content, "file", "test")
			if len(got) != tc.want {
				t.Fatalf("got %d findings, want %d", len(got), tc.want)
			}
			for _, f := range got {
				if f.Code != FindingHTMLComment {
					t.Errorf("code = %q, want %q", f.Code, FindingHTMLComment)
				}
			}
		})
	}
}

func TestScanZeroWidth(t *testing.T) {
	content := "visible" + string('\u200B') + string('\u200D') + "after"
	got := ScanZeroWidth(content, "file", "test")
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	if got[0].Code != FindingZeroWidth {
		t.Errorf("code = %q", got[0].Code)
	}
	if got[0].SourceRange == nil || got[0].SourceRange.Line != 1 || got[0].SourceRange.Column != 8 {
		t.Errorf("unexpected range %+v", got[0].SourceRange)
	}
}

func TestScanUnsafeBidi(t *testing.T) {
	content := "left-to-right" + string('\u202E') + "override"
	got := ScanUnsafeBidi(content, "file", "test")
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	if got[0].Code != FindingUnsafeBidi {
		t.Errorf("code = %q", got[0].Code)
	}
}

func TestScanTerminalControl(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"bell", "hello\x07world", 1},
		{"ansi", "\x1b[31mred\x1b[0m", 2},
		{"osc", "\x1b]0;title\x07", 1},
		{"clean", "hello\tworld\n", 0},
		{"c0", "\x00\x01\x02", 1},
		{"del", "hello\x7fworld", 1},
		{"c1_csi", "abc\u009b31mdef", 1},
		{"c1_raw", "abc\x9bdef", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanTerminalControl(tc.content, "file", "test")
			if len(got) != tc.want {
				t.Fatalf("got %d findings, want %d: %v", len(got), tc.want, got)
			}
		})
	}
}

func TestScanDangerousURLs(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"plain", "javascript:alert(1)", 1},
		{"data", "data:text/html,<script>", 1},
		{"file", "file:///etc/passwd", 1},
		{"vbscript", "vbscript:msgbox", 1},
		{"https", "https://example.com", 0},
		{"mixedcase", "jAvAsCrIpT:doIt()", 1},
		{"percent", "javascript%3aalert(1)", 1},
		{"htmlhex", "javascript&#x3a;alert(1)", 1},
		{"htmldec", "javascript&#58;alert(1)", 1},
		{"htmlnamed", "javascript&colon;alert(1)", 1},
		{"backslash", "javascript\\x3aalert(1)", 1},
		{"fullpercent", "%6a%61%76%61%73%63%72%69%70%74%3aalert(1)", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanDangerousURLs(tc.content, "file", "test")
			if len(got) != tc.want {
				t.Fatalf("got %d findings, want %d", len(got), tc.want)
			}
		})
	}
}

// dangerousURLAttacks are payloads that must keep producing a reject-worthy
// finding, including the encoded and whitespace-padded variants an attacker
// uses to evade a scheme match.
var dangerousURLAttacks = []struct {
	name    string
	content string
	scheme  string
}{
	{"javascript_plain", "javascript:alert(1)", "javascript"},
	{"javascript_percent", "javascript%3Aalert(1)", "javascript"},
	{"javascript_entity_scheme", "java&#115;cript:alert(1)", "javascript"},
	{"javascript_mixed_case", "JaVaScRiPt:alert(1)", "javascript"},
	{"javascript_entity_colon", "javascript&#x3a;alert(1)", "javascript"},
	{"javascript_decimal_entity", "javascript&#58;alert(1)", "javascript"},
	{"javascript_named_entity", "javascript&colon;alert(1)", "javascript"},
	{"javascript_backslash_hex", `javascript\x3aalert(1)`, "javascript"},
	{"javascript_fully_encoded", "%6a%61%76%61%73%63%72%69%70%74%3aalert(1)", "javascript"},
	{"javascript_whitespace_body", "javascript:\n alert(1)", "javascript"},
	{"javascript_encoded_whitespace_body", "javascript%3a%0a%20alert(1)", "javascript"},
	{"javascript_comment_body", "javascript:/*x*/alert(1)", "javascript"},
	{"javascript_empty_statement_body", "javascript:;alert(1)", "javascript"},
	{"javascript_template_body", "javascript:`${alert(1)}`", "javascript"},
	{"javascript_in_href", `<a href="javascript:alert(1)">x</a>`, "javascript"},
	{"vbscript_plain", "vbscript:msgbox(1)", "vbscript"},
	{"vbscript_percent", "vbscript%3Amsgbox(1)", "vbscript"},
	{"data_base64_entity", "data&#x3a;text/html;base64,PHNjcmlwdD4=", "data"},
	{"data_base64_percent", "data%3Atext/html;base64,PHNjcmlwdD4=", "data"},
	{"data_base64_plain", "data:text/html;base64,PHNjcmlwdD4=", "data"},
	{"data_media_type", "data:text/html,<script>alert(1)</script>", "data"},
	{"data_no_media_type", "data:,alert", "data"},
	{"data_charset_param", "data:text/html;charset=utf-8,<script>", "data"},
	{"data_base64_no_comma", "data:text/html;base64", "data"},
	{"data_space_before_media_type", "data: text/html,<script>alert(1)</script>", "data"},
	{"data_tab_before_media_type", "data:\ttext/html,<script>", "data"},
	{"data_encoded_space", "data:%20text/html;base64,PHNjcmlwdD4=", "data"},
	{"data_svg", "data:image/svg+xml;base64,PHN2Zz4=", "data"},
	{"file_triple_slash", "file:///etc/passwd", "file"},
	{"file_single_slash", "file:/etc/shadow", "file"},
	{"file_backslash", `file:\\host\share`, "file"},
	{"file_drive_letter", "file:C:/Windows/System32", "file"},
	{"file_percent", "file%3A%2F%2F%2Fetc%2Fpasswd", "file"},
}

func TestScanDangerousURLsTruePositives(t *testing.T) {
	for _, tc := range dangerousURLAttacks {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanDangerousURLs(tc.content, "file", "test")
			if len(got) == 0 {
				t.Fatalf("expected at least one finding for %q, got none", tc.content)
			}
			var matched bool
			for _, f := range got {
				if f.Code != FindingDangerousURL {
					t.Errorf("code = %q, want %q", f.Code, FindingDangerousURL)
				}
				if f.Message == "dangerous URL scheme: "+tc.scheme+":" {
					matched = true
				}
			}
			if !matched {
				t.Fatalf("no finding reported scheme %q for %q: %v", tc.scheme, tc.content, got)
			}
		})
	}
}

// markdownProseURLFixture mirrors a real agent resource body (the shape that
// produced 45 reject findings in a single live source): "data" and "file" used
// as English words followed by a colon.
const markdownProseURLFixture = "---\n" +
	"name: summarize-spreadsheet\n" +
	"description: Summarize an uploaded spreadsheet\n" +
	"---\n" +
	"\n" +
	"# Summarize spreadsheet\n" +
	"\n" +
	"**With data:** User uploads a CSV/spreadsheet and asks for a summary.\n" +
	"Without data: describe the analysis you need and I will ask for it.\n" +
	"\n" +
	"## Inputs\n" +
	"\n" +
	"- file: config.yaml\n" +
	"- Attach the file: report.pdf\n" +
	"- Input data: name, age, email\n" +
	"\n" +
	"See [the guide](https://example.com/guide) for the output format.\n" +
	"Return the data: it should be a JSON object.\n"

// TestScanDangerousURLsProseFalsePositives covers the real-world prose that
// made marketplace.find.dangerous-url the second-largest reject blocker in a
// live catalog: "data" and "file" are ordinary English words that
// documentation follows with a colon.
func TestScanDangerousURLsProseFalsePositives(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"bold_with_data", "**With data:** User uploads a CSV/spreadsheet"},
		{"without_data", "Without data: describe the analysis you need"},
		{"file_config", "file: config.yaml"},
		{"attach_file", "Attach the file: report.pdf"},
		{"bold_javascript_label", "- **JavaScript:** Node 18+ required"},
		{"data_sentence", "Return the data: it should be a JSON object."},
		{"input_data_columns", "Input data: name, age, email"},
		{"data_slash_word", "Input data: CSV/spreadsheet exports only"},
		{"data_and_file_words", "Read the input data: rows and columns; then the file: name"},
		{"frontmatter_file", "file:\n  path: docs/guide.md"},
		{"table_cell_data", "| data: | the raw rows |"},
		{"vbscript_label", "**VBScript:** not supported"},
		{"https_url", "https://example.com/data:stuff"},
		{"markdown_body", markdownBodyFixture},
		{"markdown_prose_urls", markdownProseURLFixture},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanDangerousURLs(tc.content, "Body", "test")
			if len(got) != 0 {
				t.Fatalf("expected no findings, got %d: %v", len(got), got)
			}
		})
	}
}

// TestScanDangerousURLsEncodedOffsets proves that a finding discovered in the
// URL-decoded view is still mapped back to the matching byte in the original,
// still-encoded content.
func TestScanDangerousURLsEncodedOffsets(t *testing.T) {
	t.Run("encoded_column", func(t *testing.T) {
		// "javascript%3Aalert(1)" starts at byte 16 => column 17.
		const content = "See the docs at javascript%3Aalert(1) please"
		got := ScanDangerousURLs(content, "Body", "test")
		if len(got) != 1 {
			t.Fatalf("got %d findings, want 1: %v", len(got), got)
		}
		if got[0].SourceRange == nil || got[0].SourceRange.Line != 1 || got[0].SourceRange.Column != 17 {
			t.Fatalf("unexpected range %+v", got[0].SourceRange)
		}
		if content[16] != 'j' {
			t.Fatalf("fixture drift: byte 16 = %q", content[16])
		}
	})

	t.Run("multiline_offset", func(t *testing.T) {
		content := "clean first line\nnote: data&#x3a;text/html;base64,PHNjcmlwdD4=\n"
		got := ScanDangerousURLs(content, "Body", "test")
		if len(got) != 1 {
			t.Fatalf("got %d findings, want 1: %v", len(got), got)
		}
		// The encoded token starts at column 7 of line 2.
		if got[0].SourceRange == nil || got[0].SourceRange.Line != 2 || got[0].SourceRange.Column != 7 {
			t.Fatalf("unexpected range %+v", got[0].SourceRange)
		}
	})

	t.Run("plain_column", func(t *testing.T) {
		got := ScanDangerousURLs("prefix file:///etc/passwd", "Body", "test")
		if len(got) != 1 {
			t.Fatalf("got %d findings, want 1: %v", len(got), got)
		}
		if got[0].SourceRange == nil || got[0].SourceRange.Column != 8 {
			t.Fatalf("unexpected range %+v", got[0].SourceRange)
		}
	})
}

func TestScanRemoteReferences(t *testing.T) {
	cases := []struct {
		content string
		want    int
	}{
		{"https://example.com", 1},
		{"http://example.com", 1},
		{"ftp://files.example.com", 1},
		{"//cdn.example.com/lib.js", 1},
		{"// comment", 0},
		{"http://example.com//path", 2},
		{"no urls", 0},
	}

	for _, tc := range cases {
		t.Run(tc.content, func(t *testing.T) {
			got := ScanRemoteReferences(tc.content, "file", "test")
			if len(got) != tc.want {
				t.Fatalf("got %d findings, want %d", len(got), tc.want)
			}
		})
	}
}

func TestScanSuspiciousPaths(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"dots", "cd ../etc", 1},
		{"tilde", "~/ssh/config", 1},
		{"windows", "C:\\Windows\\System32", 1},
		{"unc", "\\\\server\\share", 1},
		{"passwd", "/etc/passwd", 1},
		{"shadow", "/etc/shadow", 1},
		{"ssh", "~/.ssh", 1},
		{"https", "https://example.com/path", 0},
		{"cdn", "//cdn.example.com/lib.js", 0},
		{"normal", "a normal sentence", 0},
		{"percentdots", "%2e%2e/%2e%2e/etc/passwd", 3},
		{"backslashdots", "\\x2e\\x2e/etc/passwd", 2},
		{"htmldots", "&#x2e;&#x2e;/etc/passwd", 2},
		{"fullpercent", "%2fetc%2fpasswd", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanSuspiciousPaths(tc.content, "file", "test")
			if len(got) != tc.want {
				t.Fatalf("got %d findings, want %d: %v", len(got), tc.want, got)
			}
		})
	}
}

// suspiciousPathAttacks are payloads that must keep producing a
// reject-worthy finding in content (prose) mode.
var suspiciousPathAttacks = []struct {
	name    string
	content string
}{
	{"traversal_unix", "../../etc/passwd"},
	{"traversal_windows", `..\..\windows`},
	{"traversal_embedded", "docs/../../secret"},
	{"traversal_bare_token", "cd .."},
	{"traversal_trailing", "zip -r out /.."},
	{"traversal_dot_padded", "....//etc/passwd"},
	{"traversal_triple_dot", ".../etc/passwd"},
	{"etc_passwd", "/etc/passwd"},
	{"etc_shadow", "/etc/shadow"},
	{"ssh_key", "~/.ssh/id_rsa"},
	{"proc_environ", "/proc/self/environ"},
	{"root_bashrc", "/root/.bashrc"},
	{"var_log", "tail /var/log/auth.log"},
	{"sys_kernel", "read /sys/kernel/notes"},
	{"dev_mem", "dd if=/dev/mem"},
	{"boot_grub", "/boot/grub/grub.cfg"},
	{"usr_bin", "/usr/bin/env python"},
	{"bin_sh", "/bin/sh -c id"},
	{"sbin_init", "/sbin/init"},
	{"home_aws", "/home/user/.aws/credentials"},
	{"windows_drive", `C:\Windows\System32`},
	{"unc_share", `\\server\share`},
	{"encoded_percent_traversal", "%2e%2e%2f"},
	{"encoded_percent_passwd", "%2fetc%2fpasswd"},
	{"encoded_html_entity", "&#x2e;&#x2e;/etc/passwd"},
	{"encoded_backslash_hex", `\x2e\x2e/`},
}

func TestScanSuspiciousPathsTruePositives(t *testing.T) {
	for _, tc := range suspiciousPathAttacks {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanSuspiciousPaths(tc.content, "file", "test")
			if len(got) == 0 {
				t.Fatalf("expected at least one finding for %q, got none", tc.content)
			}
			for _, f := range got {
				if f.Code != FindingSuspiciousPath {
					t.Errorf("code = %q, want %q", f.Code, FindingSuspiciousPath)
				}
			}
		})
	}
}

// markdownBodyFixture mirrors a real agent command body (the shape that
// produced 7 false positives against a live catalog): slash-command
// invocations and prose ellipses.
const markdownBodyFixture = "---\n" +
	"name: analyze-cohorts\n" +
	"description: Analyze user cohorts and summarize retention\n" +
	"---\n" +
	"\n" +
	"# /analyze-cohorts\n" +
	"\n" +
	"Invoke this command with `/analyze-cohorts`, then `/sync` and `/sysinfo`.\n" +
	"\n" +
	"## Steps\n" +
	"\n" +
	"1. Collect the raw events\n" +
	"2. ...\n" +
	"3. Summarize the cohorts and so on...\n" +
	"\n" +
	"| Cohort | Retention | Notes |\n" +
	"|--------|-----------|-------|\n" +
	"| week-1 | 42%       | ...   |\n" +
	"| week-2 | 37%       | ....  |\n" +
	"\n" +
	"Related commands: /deploy, /review-pr, /devcontainer, /analyze-cohorts.\n" +
	"See [the guide](/docs/guide/setup) for details.\n" +
	"Run `go test ./...` and `go build ./...` before you finish.\n"

func TestScanSuspiciousPathsProseFalsePositives(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"markdown_body", markdownBodyFixture},
		{"slash_command", "/analyze-cohorts"},
		{"slash_commands_inline", "Use /deploy or /review-pr to continue."},
		{"slash_command_args", "/analyze-cohorts --since 30d"},
		{"slash_command_sensitive_prefix", "/dev-setup /devcontainer /system /rootcause /binary"},
		{"ellipsis", "..."},
		{"long_ellipsis", "...."},
		{"list_continuation", "2. ..."},
		{"table_cell", "| ... |"},
		{"trailing_ellipsis", "and so on..."},
		{"go_wildcard", "go test ./... && go build ./..."},
		{"git_range", "git log main..HEAD"},
		{"numeric_range", "for i in 1..5"},
		{"version", "version 1.2.3"},
		{"root_relative_link", "[guide](/docs/guide/setup)"},
		{"word_with_root_prefix", "the /etcetera directory is fictional"},
		{"escaped_backslash", `the regex \\d+ matches digits`},
		{"url", "https://example.com/etcetera/path"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanSuspiciousPaths(tc.content, "Body", "test")
			if len(got) != 0 {
				t.Fatalf("expected no findings, got %d: %v", len(got), got)
			}
		})
	}
}

func TestScanSuspiciousFilePathsStrict(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"absolute_sensitive", "/etc/cron.d/payload", true},
		{"absolute_plain", "/opt/tools/run.txt", true},
		{"traversal", "../../escape", true},
		{"traversal_nested", "assets/../../escape", true},
		{"traversal_windows", `..\..\escape`, true},
		{"bare_traversal", "..", true},
		{"unc", `\\server\share\file`, true},
		{"drive", `C:\Windows\System32\cmd`, true},
		{"relative_clean", "docs/guide.md", false},
		{"relative_nested", "assets/img/logo.png", false},
		{"dotted_name", "analyze-cohorts.md", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanSuspiciousFilePaths(tc.path, tc.path, "test")
			if tc.want && len(got) == 0 {
				t.Fatalf("expected a finding for %q, got none", tc.path)
			}
			if !tc.want && len(got) != 0 {
				t.Fatalf("expected no finding for %q, got %v", tc.path, got)
			}
		})
	}
}

// TestScanAssetsFilePathsStayStrict proves the FileSet call site keeps the
// strict profile: a slash-command-looking body is fine in prose, but an
// absolute or traversing archive entry is still reported.
func TestScanAssetsFilePathsStayStrict(t *testing.T) {
	files := FileSet{
		"/etc/cron.d/payload": []byte("bad"),
		"../../escape.md":     []byte("bad"),
		"commands/analyze-cohorts.md": []byte("# /analyze-cohorts\n" +
			"Invoke with `/analyze-cohorts`. Then ...\n"),
	}

	byFile := make(map[string]int)
	for _, f := range ScanAssets(files, "test") {
		if f.Code != FindingSuspiciousPath || f.SourceRange == nil {
			continue
		}
		byFile[f.SourceRange.File]++
	}

	if byFile["/etc/cron.d/payload"] == 0 {
		t.Errorf("absolute archive entry not reported")
	}
	if byFile["../../escape.md"] == 0 {
		t.Errorf("traversing archive entry not reported")
	}
	if n := byFile["commands/analyze-cohorts.md"]; n != 0 {
		t.Errorf("clean archive entry reported %d times", n)
	}
}

// TestScanSuspiciousPathsEncodedOffsets proves that findings discovered in the
// URL-decoded view are still mapped back to the original byte range.
func TestScanSuspiciousPathsEncodedOffsets(t *testing.T) {
	t.Run("decoded_columns", func(t *testing.T) {
		// "/etc/passwd" starts at byte 12 (column 13) in the raw content; the
		// decoded "../" maps back to byte 0 (column 1).
		got := ScanSuspiciousPaths("&#x2e;&#x2e;/etc/passwd", "Body", "test")
		if len(got) != 2 {
			t.Fatalf("got %d findings, want 2: %v", len(got), got)
		}
		cols := map[int]bool{}
		for _, f := range got {
			if f.SourceRange == nil || f.SourceRange.Line != 1 {
				t.Fatalf("unexpected range %+v", f.SourceRange)
			}
			cols[f.SourceRange.Column] = true
		}
		if !cols[1] || !cols[13] {
			t.Fatalf("columns = %v, want {1, 13}", cols)
		}
	})

	t.Run("multiline_offset", func(t *testing.T) {
		content := "clean first line\nnote: %2e%2e%2fetc%2fpasswd\n"
		got := ScanSuspiciousPaths(content, "Body", "test")
		if len(got) == 0 {
			t.Fatal("expected findings for encoded traversal")
		}
		for _, f := range got {
			if f.SourceRange == nil || f.SourceRange.Line != 2 {
				t.Fatalf("finding %q outside encoded token: %+v", f.Message, f.SourceRange)
			}
			// The encoded token starts at column 7 of line 2.
			if f.SourceRange.Column < 7 {
				t.Fatalf("column = %d, want >= 7 (%+v)", f.SourceRange.Column, f.SourceRange)
			}
		}
	})
}

func TestScanObfuscatedInstructions(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"eval", "eval("},
		{"system", "system("},
		{"base64", base64Of("javascript:alert(1)")},
		{"hex", hexOf("eval")},
		{"percent", "%65%76%61%6c"},
		{"htmlentity", "&#x65;&#x76;&#x61;&#x6c;"},
		{"backslash", "\\x65\\x76\\x61\\x6c"},
		{"homoglyph", "Аpple"}, // Cyrillic А + Latin pple
		// Adversarial bypasses.
		{"percent_script", "jav%61script:alert(1)"},
		{"backslash_eval", "ev\\x61l("},
		{"html_url", "javascript&#x3a;alert(1)"},
		{"short_base64", "ZXZhbA"},
		{"upper_hex", "0X6576616c"},
		{"unicode_ws", "eval\u00a0("},
		{"js_comment", "eval/*x*/("},
		{"eval_dot_call", "eval.call(...)"},
		{"window_eval", "window[\"eval\"]"},
		{"setTimeout", "setTimeout(\"...\",0)"},
		{"new_function", "new Function(...)"},
		{"exec_rm", "exec(\"rm -rf /\")"},
		{"cyrillic_eval", "еval"}, // Cyrillic е + Latin val
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanObfuscatedInstructions(tc.content, "file", "test")
			if len(got) == 0 {
				t.Fatalf("expected at least one finding, got none")
			}
			for _, f := range got {
				if f.Code != FindingObfuscatedInstruction {
					t.Errorf("code = %q, want %q", f.Code, FindingObfuscatedInstruction)
				}
			}
		})
	}
}

func base64Of(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func hexOf(s string) string {
	return "0x" + hex.EncodeToString([]byte(s))
}
